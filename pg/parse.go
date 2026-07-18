package pg

import (
	"strconv"
	"strings"

	"fiatjaf.com/nostr"

	"github.com/auxten/postgresql-parser/pkg/sql/sem/tree"
	"github.com/jackc/pgx/v5/pgproto3"
)

// columnInfo is the (name, OID) pair sent in a RowDescription.
type columnInfo struct {
	Name string
	OID  uint32
}

func rowDescFromColumns(cols []columnInfo) *pgproto3.RowDescription {
	fd := make([]pgproto3.FieldDescription, len(cols))
	for i, c := range cols {
		fd[i] = pgproto3.FieldDescription{
			Name:         []byte(c.Name),
			DataTypeOID:  c.OID,
			DataTypeSize: -1,
			TypeModifier: -1,
			Format:       0,
		}
	}
	return &pgproto3.RowDescription{Fields: fd}
}

// resTarget is the resolved SELECT target-ish entry: a column name plus an
// optional table-prefixed column. Used by select planning & catalog response
// building.
type resTarget struct {
	Alias string
	Value string
}

// catalogTargets returns the list of qualified catalog tables referenced in
// the FROM clause ("schema.table", lowercase) plus any unknown relation names.
// Joins are flattened; subqueries are skipped.
func catalogTargets(tables tree.TableExprs) ([]string, []string) {
	known := []string{}
	unknown := []string{}
	for _, t := range tables {
		switch te := t.(type) {
		case *tree.AliasedTableExpr:
			switch inner := te.Expr.(type) {
			case *tree.TableName:
				known, unknown = pushTable(known, unknown, inner.Schema(), inner.TableName.String())
			case *tree.JoinTableExpr:
				known, unknown = pushTables(known, unknown, tree.TableExprs{inner.Left, inner.Right})
			case *tree.ParenTableExpr:
				known, unknown = pushTables(known, unknown, tree.TableExprs{inner.Expr})
			}
		case *tree.JoinTableExpr:
			known, unknown = pushTables(known, unknown, tree.TableExprs{te.Left, te.Right})
		case *tree.ParenTableExpr:
			known, unknown = pushTables(known, unknown, tree.TableExprs{te.Expr})
		}
	}
	return known, unknown
}

func pushTables(known, unknown []string, tables tree.TableExprs) ([]string, []string) {
	k, u := catalogTargets(tables)
	return append(known, k...), append(unknown, u...)
}

func pushTable(known, unknown []string, schema, table string) ([]string, []string) {
	schema = strings.ToLower(schema)
	table = strings.ToLower(table)
	if schema == "" {
		// treat unqualified as a user table (layer). Track as "unknown" candidate.
		return known, append(unknown, table)
	}
	return append(known, schema+"."+table), unknown
}

// singleEventTable returns the layer name when the FROM clause has exactly one
// relation that resolves to a known pyramid layer (case-insensitive, dashes).
func singleEventTable(s *Server, from []string) string {
	for _, name := range from {
		if s.LayerByName(name) != nil {
			return name
		}
	}
	return ""
}

// --- target list ---------------------------------------------------------------

type targetKind int

const (
	targetAll targetKind = iota
	targetColumns
	targetCount
	targetOther
)

type selectTargetInfo struct {
	kind    targetKind
	columns []columnInfo
}

func buildTarget(exprs tree.SelectExprs) selectTargetInfo {
	out := selectTargetInfo{kind: targetOther}
	countSeen := false

	// Star works only for SELECT * FROM layer. Otherwise unpack columns.
	for _, se := range exprs {
		if isStar(se.Expr) {
			out.kind = targetAll
			out.columns = append(out.columns, eventColumnsToColumnInfo()...)
			continue
		}
		colName := string(se.As)
		switch e := se.Expr.(type) {
		case *tree.UnresolvedName:
			if e.Star {
				out.kind = targetAll
				out.columns = append(out.columns, eventColumnsToColumnInfo()...)
				continue
			}
			name := ""
			if e.NumParts >= 1 {
				name = e.Parts[0]
			}
			if colName == "" {
				colName = name
			}
			if strings.EqualFold(name, "count") {
				countSeen = true
			}
			out.columns = append(out.columns, columnInfo{Name: colName, OID: columnDefaultOID(name)})
		case *tree.FuncExpr:
			fn := ""
			if ref, ok := e.Func.FunctionReference.(*tree.UnresolvedName); ok && ref.NumParts >= 1 {
				fn = strings.ToLower(ref.Parts[0])
			}
			if colName == "" {
				colName = fn
			}
			if fn == "count" {
				countSeen = true
				out.columns = append(out.columns, columnInfo{Name: "count", OID: Int8OID})
				continue
			}
			out.columns = append(out.columns, columnInfo{Name: colName, OID: TextOID})
		case *tree.NumVal, *tree.DString:
			if colName == "" {
				colName = "?column?"
			}
			out.columns = append(out.columns, columnInfo{Name: colName, OID: TextOID})
		default:
			if colName == "" {
				colName = "?column?"
			}
			out.columns = append(out.columns, columnInfo{Name: colName, OID: TextOID})
		}
	}

	if countSeen && len(out.columns) == 1 {
		out.kind = targetCount
		return out
	}
	if out.kind == targetAll {
		return out
	}
	out.kind = targetColumns
	return out
}

func columnDefaultOID(name string) uint32 {
	switch strings.ToLower(name) {
	case "kind":
		return Int4OID
	case "created_at":
		return Int8OID
	case "id", "pubkey", "content", "tags", "sig":
		return TextOID
	case "oid":
		return OIDOID
	default:
		return TextOID
	}
}

func isStar(e tree.Expr) bool {
	if u, ok := e.(*tree.UnresolvedName); ok {
		return u.Star
	}
	if _, ok := e.(*tree.UnqualifiedStar); ok {
		return true
	}
	return false
}

// --- filter -------------------------------------------------------------------

// buildFilter walks the WHERE clause and extracts the (subset of nostr.Filter
// fields) we understand: kind=, pubkey=, id=, content LIKE / =, since=, until=,
// limit=, and tags[h]=. AND combines; OR is ignored (we keep both branches).
func buildFilter(w *tree.Where) nostr.Filter {
	f := nostr.Filter{}
	if w == nil || w.Expr == nil {
		return f
	}
	walkWhere(w.Expr, &f)
	return f
}

func walkWhere(e tree.Expr, f *nostr.Filter) {
	switch ex := e.(type) {
	case *tree.AndExpr:
		walkWhere(ex.Left, f)
		walkWhere(ex.Right, f)
	case *tree.OrExpr:
		// OR semantics are too complex; just keep one branch (better than no rows).
		walkWhere(ex.Left, f)
	case *tree.NotExpr:
		// ignore negation — we don't expose searchable conditions meaningfully.
	case *tree.ComparisonExpr:
		applyComparison(ex, f)
	case *tree.ParenExpr:
		walkWhere(ex.Expr, f)
	}
}

func applyComparison(ex *tree.ComparisonExpr, f *nostr.Filter) {
	col := columnOf(ex.Left)
	if col == "" {
		col = columnOf(ex.Right)
	}
	if col == "" {
		return
	}
	switch strings.ToLower(col) {
	case "kind":
		if v, ok := intFrom(ex.Right); ok {
			if !containsKind(f.Kinds, nostr.Kind(v)) {
				f.Kinds = append(f.Kinds, nostr.Kind(v))
			}
		}
	case "pubkey":
		if v, ok := strFrom(ex.Right); ok {
			if pk, err := nostr.PubKeyFromHex(v); err == nil {
				if !containsPub(f.Authors, pk) {
					f.Authors = append(f.Authors, pk)
				}
			}
		}
	case "id":
		if v, ok := strFrom(ex.Right); ok {
			if id, err := nostr.IDFromHex(v); err == nil {
				if !containsID(f.IDs, id) {
					f.IDs = append(f.IDs, id)
				}
			}
		}
	case "content":
		if ex.Operator == tree.Like || ex.Operator == tree.ILike || ex.Operator == tree.RegMatch || ex.Operator == tree.RegIMatch {
			if v, ok := strFrom(ex.Right); ok {
				f.Search = trimLikePattern(v)
			}
		} else if v, ok := strFrom(ex.Right); ok {
			f.Search = v
		}
	case "since":
		if v, ok := intFrom(ex.Right); ok {
			f.Since = nostr.Timestamp(v)
		}
	case "until":
		if v, ok := intFrom(ex.Right); ok {
			f.Until = nostr.Timestamp(v)
		}
	case "limit":
		if v, ok := intFrom(ex.Right); ok {
			f.Limit = v
		}
	}
}

func containsKind(ks []nostr.Kind, k nostr.Kind) bool {
	for _, e := range ks {
		if e == k {
			return true
		}
	}
	return false
}

func containsPub(pks []nostr.PubKey, pk nostr.PubKey) bool {
	for _, e := range pks {
		if e == pk {
			return true
		}
	}
	return false
}

func containsID(ids []nostr.ID, id nostr.ID) bool {
	for _, e := range ids {
		if e == id {
			return true
		}
	}
	return false
}

func columnOf(e tree.Expr) string {
	if e == nil {
		return ""
	}
	if u, ok := e.(*tree.UnresolvedName); ok && u.NumParts >= 1 {
		return u.Parts[0]
	}
	if c, ok := e.(*tree.ColumnItem); ok {
		return string(c.ColumnName)
	}
	return ""
}

func strFrom(e tree.Expr) (string, bool) {
	switch v := e.(type) {
	case *tree.DString:
		return string(*v), true
	case *tree.StrVal:
		return v.RawString(), true
	case *tree.NumVal:
		return v.ExactString(), true
	case *tree.ParenExpr:
		return strFrom(v.Expr)
	case *tree.CastExpr:
		return strFrom(v.Expr)
	}
	return "", false
}

func intFrom(e tree.Expr) (int, bool) {
	if v, ok := e.(*tree.NumVal); ok {
		i, err := v.AsInt64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	}
	if v, ok := e.(*tree.DInt); ok {
		return int(*v), true
	}
	if v, ok := e.(*tree.ParenExpr); ok {
		return intFrom(v.Expr)
	}
	return 0, false
}

// trimLikePattern strips the surrounding LIKE/VARCHAR wildcards (% and _) and
// regex anchors ('^...$') so callers can plug it into a string search.
func trimLikePattern(s string) string {
	s = strings.Trim(s, "%")
	if strings.HasPrefix(s, "^") {
		s = s[1:]
	}
	if strings.HasSuffix(s, "$") {
		s = s[:len(s)-1]
	}
	return s
}

// limitOf parses LIMIT clause if present.
func limitOf(sel *tree.Select) int {
	if sel == nil || sel.Limit == nil || sel.Limit.Count == nil {
		return 0
	}
	if v, ok := intFrom(sel.Limit.Count); ok {
		return v
	}
	return 0
}

// intString helper.
func intString(n int) string { return strconv.Itoa(n) }

// inferExprName picks a reasonable column name for a SELECT expression that
// has no explicit alias (eg. `c.oid` -> "oid", `count(*)` -> "count").
func inferExprName(e tree.Expr) string {
	switch ex := e.(type) {
	case *tree.UnresolvedName:
		if ex.NumParts >= 1 {
			// Parts are stored as [column, table, schema, catalog]; Parts[0] is
			// always the rightmost (column) name. See [tree.UnresolvedName].
			return ex.Parts[0]
		}
	case *tree.ColumnItem:
		return string(ex.ColumnName)
	case *tree.FuncExpr:
		if ref, ok := ex.Func.FunctionReference.(*tree.UnresolvedName); ok && ref.NumParts >= 1 {
			return ref.Parts[0]
		}
	case *tree.NumVal:
		return "?column?"
	}
	return "?column?"
}
