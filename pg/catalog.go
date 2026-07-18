package pg

import (
	"strconv"
	"strings"

	"github.com/auxten/postgresql-parser/pkg/sql/sem/tree"
	"github.com/jackc/pgx/v5/pgproto3"
)

// execCatalogSelect is the entry point for any SELECT/FROM query that
// references at least one pg_catalog.* (or system) table. Returns true when
// handled, false to fall back to the generic empty-response path.
func (s *Server) execCatalogSelect(be *pgproto3.Backend, sel *tree.Select, clause *tree.SelectClause, from []string) bool {
	aliasColumns := targetsAsColumns(clause.Exprs)

	// Tables we never populate — short circuit before pg_class matching.
	switch {
	case hasFrom(from, "pg_catalog.pg_inherits"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	case hasFrom(from, "pg_catalog.pg_index"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	case hasFrom(from, "pg_catalog.pg_constraint"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	case hasFrom(from, "pg_catalog.pg_partitioned_table"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	}

	switch {
	case hasFrom(from, "pg_catalog.pg_class"):
		s.handlePgClass(be, clause, aliasColumns)
		return true
	case hasFrom(from, "pg_catalog.pg_attribute"):
		s.handlePgAttribute(be, clause, aliasColumns)
		return true
	case hasFrom(from, "pg_catalog.pg_namespace"):
		s.handlePgNamespace(be, clause, aliasColumns)
		return true
	case hasFrom(from, "pg_catalog.pg_database"):
		s.handlePgDatabase(be, clause, aliasColumns)
		return true
	case hasFrom(from, "pg_catalog.pg_proc"),
		hasFrom(from, "pg_catalog.pg_type"),
		hasFrom(from, "pg_catalog.pg_views"),
		hasFrom(from, "pg_catalog.pg_matviews"),
		hasFrom(from, "pg_catalog.pg_settings"),
		hasFrom(from, "pg_catalog.pg_description"),
		hasFrom(from, "pg_catalog.pg_attrdef"),
		hasFrom(from, "pg_catalog.pg_rewrite"),
		hasFrom(from, "pg_catalog.pg_stat_activity"),
		hasFrom(from, "pg_catalog.pg_am"),
		hasFrom(from, "pg_catalog.pg_event_trigger"),
		hasFrom(from, "pg_catalog.pg_roles"),
		hasFrom(from, "pg_catalog.pg_user"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	case hasFrom(from, "information_schema"):
		s.handleEmpty(be, aliasColumns, "SELECT 0")
		return true
	}
	return false
}

// ----------------------------------------------------------------- pg_class ---

func (s *Server) handlePgClass(be *pgproto3.Backend, clause *tree.SelectClause, cols []columnInfo) {
	tableName, single := extractRelnameFilter(clause.Where)
	if !single {
		// Fall back to `c.oid = '<int>'` lookup (used by psql after step 1).
		name := extractOIDFilter(clause.Where)
		if name != "" {
			tableName = name
			single = true
		}
	}

	if single {
		// "\d <name>" relation-summary query: zero or one row describing the
		// target table.
		layer := s.LayerByName(tableName)
		if layer == nil {
			s.handleEmpty(be, cols, "SELECT 0")
			return
		}
		row := make([][]byte, len(cols))
		oid := layerOID(layer.Name)
		for i, c := range cols {
			row[i] = []byte(synthesizeColumnValue(c.Name, layer.Name, oid))
		}
		be.Send(rowDescFromColumns(cols))
		be.Send(&pgproto3.DataRow{Values: row})
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		return
	}

	// "\d" or "\dt": one row per virtual table.
	be.Send(rowDescFromColumns(cols))
	n := 0
	for i := range s.Layers {
		layer := &s.Layers[i]
		row := make([][]byte, len(cols))
		oid := layerOID(layer.Name)
		for j, c := range cols {
			row[j] = []byte(synthesizeColumnValue(c.Name, layer.Name, oid))
		}
		be.Send(&pgproto3.DataRow{Values: row})
		n++
	}
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT " + intString(n))})
}

// synthesizeColumnValue produces a value for a single pg_class-style column
// alias. Unknown aliases are emitted as an empty string (psql's "Null" is
// handled through column-level presence, not value).
func synthesizeColumnValue(alias, tableName string, oid int) string {
	switch alias {
	case "Schema", "schema", "nspname":
		return "public"
	case "Name", "name", "relname":
		return tableName
	case "Type", "type":
		return "table"
	case "Owner", "owner":
		return "pyramid"
	case "relkind":
		return "r"
	case "relpersistence":
		return "p"
	case "oid":
		return intString(oid)
	case "relrowsecurity", "relforcerowsecurity", "relispartition",
		"hasoids", "relhasoids", "relchecks", "relhasindex",
		"relhasrules", "relhastriggers", "relispopulated",
		"relhassubtrans":
		return "f"
	case "relnatts":
		return intString(len(eventColumns))
	case "reltablespace", "relpartbound", "reloptions", "config",
		"options", "expression":
		return ""
	case "relowner":
		return "10"
	case "relreplident":
		return "d"
	case "amname":
		return "heap"
	case "reloftype":
		return "0"
	default:
		return ""
	}
}

// --------------------------------------------------------------- pg_attribute --

func (s *Server) handlePgAttribute(be *pgproto3.Backend, clause *tree.SelectClause, cols []columnInfo) {
	tableName := extractAttrelidFilter(clause.Where)
	if tableName == "" {
		tableName, _ = extractRelnameFilter(clause.Where)
	}
	layer := s.LayerByName(tableName)
	if layer == nil {
		s.handleEmpty(be, cols, "SELECT 0")
		return
	}

	be.Send(rowDescFromColumns(cols))
	for i, ec := range eventColumns {
		row := make([][]byte, len(cols))
		for j, c := range cols {
			row[j] = []byte(synthesizeAttributeColumn(c.Name, ec, i+1))
		}
		be.Send(&pgproto3.DataRow{Values: row})
	}
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT " + intString(len(eventColumns)))})
}

func synthesizeAttributeColumn(alias string, ec eventColumn, attnum int) string {
	switch alias {
	case "Column", "name", "attname":
		return ec.Name
	case "Type", "type", "format_type":
		return pgTypeName(ec.Type)
	case "Default", "default":
		return ""
	case "Null", "null", "Nullable", "nullable":
		return ""
	case "not_null":
		return "f"
	case "Storage", "storage":
		return "plain"
	case "Stats target", "stats_target", "attstattarget":
		return "-"
	case "Collation", "collation":
		return ""
	case "attnum":
		return intString(attnum)
	case "attnotnull":
		return "f"
	case "atthasdef":
		return "f"
	case "attisdropped":
		return "f"
	case "attidentity":
		return ""
	case "attgenerated":
		return ""
	case "atttypid":
		return intString(int(ec.Type))
	case "Description", "description":
		return ""
	default:
		return ""
	}
}

func pgTypeName(t pgType) string {
	switch t {
	case Int4:
		return "integer"
	case Int8:
		return "bigint"
	case 21:
		return "smallint"
	case Text:
		return "text"
	case Bool:
		return "boolean"
	case OID:
		return "oid"
	default:
		return "text"
	}
}

// --------------------------------------------------------------- pg_namespace -

func (s *Server) handlePgNamespace(be *pgproto3.Backend, clause *tree.SelectClause, cols []columnInfo) {
	be.Send(rowDescFromColumns(cols))
	be.Send(&pgproto3.DataRow{Values: rowFor(cols, map[string]string{
		"nspname": "public", "Name": "public", "name": "public",
		"oid": intString(2200), "nspowner": "10",
	})})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
}

// --------------------------------------------------------------- pg_database --

func (s *Server) handlePgDatabase(be *pgproto3.Backend, clause *tree.SelectClause, cols []columnInfo) {
	be.Send(rowDescFromColumns(cols))
	be.Send(&pgproto3.DataRow{Values: rowFor(cols, map[string]string{
		"Name": "pyramid", "datname": "pyramid", "name": "pyramid",
		"Owner": "pyramid", "datdba": "10",
		"Encoding": "UTF8", "encoding": "UTF8",
		"Collate": "C", "Ctype": "C",
	})})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
}

// --------------------------------------------------------------- helpers -----

func (s *Server) handleEmpty(be *pgproto3.Backend, cols []columnInfo, tag string) {
	if len(cols) == 0 {
		cols = []columnInfo{{Name: "?column?", OID: TextOID}}
	}
	be.Send(rowDescFromColumns(cols))
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
}

func hasFrom(from []string, table string) bool {
	t := strings.ToLower(table)
	for _, f := range from {
		if f == t {
			return true
		}
	}
	return false
}

func layerOID(name string) int {
	// fake but stable OIDs so psql joins on oid work-ish.
	return 16384 + int(hashStr(strings.ToLower(strings.ReplaceAll(name, "_", ""))))
}

func hashStr(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// targetsAsColumns extracts the per-target RowDescription columns (alias names
// may be empty for label-less expressions; we fall back to inferring the name
// from the column reference, or "?column?" as a last resort).
func targetsAsColumns(exprs tree.SelectExprs) []columnInfo {
	cols := make([]columnInfo, 0, len(exprs))
	for _, se := range exprs {
		name := string(se.As)
		if name == "" {
			name = inferExprName(se.Expr)
		}
		oid := uint32(TextOID)
		switch strings.ToLower(name) {
		case "oid", "atttypid", "nspoid", "relowner", "datdba", "nspowner":
			oid = OIDOID
		case "attnum", "relnatts", "relpages", "reltuples":
			oid = Int4OID
		case "count":
			oid = Int8OID
		case "attnotnull", "atthasdef", "attisdropped", "relrowsecurity", "relforcerowsecurity":
			oid = BoolOID
		}
		cols = append(cols, columnInfo{Name: name, OID: oid})
	}
	return cols
}

// extractRelnameFilter finds the `c.relname ~ '^name$'` (or `c.relname = name`)
// pattern inside a WHERE clause and returns (name, true); else ("", false).
func extractRelnameFilter(w *tree.Where) (string, bool) {
	if w == nil {
		return "", false
	}
	got := ""
	walkLookups(w.Expr, &whereProbe{
		onEq: func(col, val string) {
			if strings.EqualFold(col, "relname") && got == "" {
				got = unanchor(val)
			}
		},
		onRegMatch: func(col, val string) {
			if strings.EqualFold(col, "relname") && got == "" {
				got = unanchor(val)
			}
		},
	})
	if got == "" {
		return "", false
	}
	return got, true
}

// extractOIDFilter finds `<col>.oid = '<int>'` (or `<col>.oid = <int>`) and
// maps the integer back to a layer name by reversing [layerOID].
func extractOIDFilter(w *tree.Where) string {
	if w == nil {
		return ""
	}
	got := ""
	walkLookups(w.Expr, &whereProbe{
		onEq: func(col, val string) {
			if !strings.EqualFold(col, "oid") && !strings.EqualFold(col, "attrelid") {
				return
			}
			if got != "" {
				return
			}
			// convert val to int, then find a layer whose layerOID matches
			i, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil {
				return
			}
			for _, l := range layersGlobal {
				if layerOID(l) == i {
					got = l
					return
				}
			}
		},
	})
	return got
}

// layersGlobal points at the live server's layer name list, so [extractOIDFilter]
// can reverse-map OIDs without holding a back-pointer per Where. Set in Start.
var layersGlobal []string

func extractAttrelidFilter(w *tree.Where) string {
	if w == nil {
		return ""
	}
	got := ""
	walkLookups(w.Expr, &whereProbe{
		onCastStr: func(col, val string) {
			if strings.EqualFold(col, "attrelid") && got == "" {
				got = val
			}
		},
		onEq: func(col, val string) {
			if !strings.EqualFold(col, "attrelid") {
				return
			}
			if got != "" {
				return
			}
			if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				for _, l := range layersGlobal {
					if layerOID(l) == i {
						got = l
						return
					}
				}
			}
		},
	})
	return got
}

// unanchor strips '^...$' from a regex pattern and surrounding quotes/wildcards.
func unanchor(p string) string {
	s := p
	s = strings.TrimPrefix(s, "^(")
	s = strings.TrimSuffix(s, ")$")
	s = strings.TrimPrefix(s, "^")
	s = strings.TrimSuffix(s, "$")
	s = strings.Trim(s, "%")
	return s
}

type whereProbe struct {
	onEq       func(col, val string)
	onRegMatch func(col, val string)
	onCastStr  func(col, val string)
}

func walkLookups(e tree.Expr, p *whereProbe) {
	if e == nil {
		return
	}
	switch ex := e.(type) {
	case *tree.AndExpr:
		walkLookups(ex.Left, p)
		walkLookups(ex.Right, p)
	case *tree.OrExpr:
		walkLookups(ex.Left, p)
		walkLookups(ex.Right, p)
	case *tree.ParenExpr:
		walkLookups(ex.Expr, p)
	case *tree.ComparisonExpr:
		probeComparison(ex, p)
	case *tree.FuncExpr:
		// pg_catalog.pg_table_is_visible(c.oid) etc. - ignore.
	case *tree.CastExpr:
		// 'name'::regclass handled by the comparison walk.
	}
}

func probeComparison(ex *tree.ComparisonExpr, p *whereProbe) {
	col := columnOf(ex.Left)
	if col == "" {
		col = columnOf(ex.Right)
	}
	if col == "" {
		return
	}
	switch ex.Operator {
	case tree.EQ:
		if v, ok := strFrom(ex.Right); ok {
			p.onEq(col, v)
		} else if v, ok := strFrom(ex.Left); ok {
			p.onEq(col, v)
		}
	case tree.RegMatch, tree.RegIMatch:
		if v, ok := strFrom(ex.Right); ok {
			p.onRegMatch(col, v)
		} else if v, ok := strFrom(ex.Left); ok {
			p.onRegMatch(col, v)
		}
	case tree.Like, tree.ILike:
		if v, ok := strFrom(ex.Right); ok {
			p.onRegMatch(col, v)
		}
	case tree.In:
		// `c.relkind IN ('r','p',...)` — not interesting for table-name lookup.
	}
	// 'name'::regclass pattern: handle the EQ on cast inner.
	if cast, ok := ex.Right.(*tree.CastExpr); ok {
		if v, ok := strFrom(cast.Expr); ok {
			// CastType string: regclass inferred from kind; we accept any cast
			// here since names never need casting otherwise.
			p.onCastStr(col, v)
		}
	}
	if cast, ok := ex.Left.(*tree.CastExpr); ok {
		if v, ok := strFrom(cast.Expr); ok {
			p.onCastStr(col, v)
		}
	}
}

func rowFor(cols []columnInfo, vals map[string]string) [][]byte {
	out := make([][]byte, len(cols))
	for i, c := range cols {
		if v, ok := vals[c.Name]; ok {
			out[i] = []byte(v)
			continue
		}
		out[i] = []byte("")
	}
	return out
}

// intString is duplicated from parse.go to avoid another import in this file.
// (parse.go exports it already; we use that.)
var _ = strconv.Itoa
