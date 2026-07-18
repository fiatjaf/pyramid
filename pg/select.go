package pg

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"

	"github.com/auxten/postgresql-parser/pkg/sql/sem/tree"
	"github.com/jackc/pgx/v5/pgproto3"
)

// eventColumn describes a column exposed by every event-backed virtual table.
type eventColumn struct {
	Name string
	Type pgType
}

var eventColumns = []eventColumn{
	{Name: "id", Type: Text},
	{Name: "pubkey", Type: Text},
	{Name: "kind", Type: Int4},
	{Name: "created_at", Type: Int8},
	{Name: "content", Type: Text},
	{Name: "tags", Type: Text}, // JSON array
	{Name: "sig", Type: Text},
}

// eventRow represents one materialized event row in fixed column order
// matching [eventColumns].
type eventRow struct {
	id        string
	pubkey    string
	kind      int
	createdAt int64
	content   string
	tags      string
	sig       string
}

// execSelect dispatches a SELECT by inspecting the FROM clause and target list.
func (s *Server) execSelect(be *pgproto3.Backend, sel *tree.Select) {
	clause, ok := sel.Select.(*tree.SelectClause)
	if !ok {
		// VALUES, UNION etc. → just respond with one empty row so callers keep
		// going (psql tolerates empty result sets).
		s.sendEmpty(be)
		return
	}
	if len(clause.From.Tables) == 0 {
		s.execScalarSelect(be, sel, clause)
		return
	}

	from, unknown := catalogTargets(clause.From.Tables)

	// pg_catalog dispatch
	if len(from) > 0 {
		if handled := s.execCatalogSelect(be, sel, clause, from); handled {
			return
		}
	}

	// event table dispatch: a single layer table (unqualified name).
	if tbl := singleEventTable(s, unknown); tbl != "" {
		s.execLayerSelect(be, sel, clause, tbl)
		return
	}

	// unknown table → return empty result honoring the target list shape
	s.sendEmpty(be)
}

// execScalarSelect handles SELECT <expr>[, ...] with no FROM (psql's
// `SELECT version()`, `SELECT 1`, libpq probes like `SELECT current_database()`).
func (s *Server) execScalarSelect(be *pgproto3.Backend, sel *tree.Select, clause *tree.SelectClause) {
	cols := make([]columnInfo, 0, len(clause.Exprs))
	vals := make([][]byte, 0, len(clause.Exprs))
	for _, se := range clause.Exprs {
		name, val := s.evalScalarExpr(se.Expr)
		cols = append(cols, columnInfo{Name: name, OID: TextOID})
		vals = append(vals, []byte(val))
	}
	be.Send(rowDescFromColumns(cols))
	be.Send(&pgproto3.DataRow{Values: vals})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
}

// execScalarConst is a degenerate variant used by SHOW CLUSTER SETTING-style
// statements that have no real target list. Emits a single anonymous text
// column with a single value.
func (s *Server) execScalarConst(be *pgproto3.Backend, val string, t pgType) {
	be.Send(rowDescFromColumns([]columnInfo{{Name: "?column?", OID: colOID(t)}}))
	be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(val)}})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
}

// execShowVar handles `SHOW <name>` by returning a small known set of values
// that psql and libpq probe at startup.
func (s *Server) execShowVar(be *pgproto3.Backend, show *tree.ShowVar) {
	cols := []columnInfo{{Name: "name", OID: colOID(Text)}, {Name: "setting", OID: colOID(Text)}}
	val := lookupSetting(strings.ToLower(show.Name))
	be.Send(rowDescFromColumns(cols))
	be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(strings.ToLower(show.Name)), []byte(val)}})
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SHOW")})
}

func lookupSetting(name string) string {
	switch name {
	case "server_version", "server_version_num":
		return "16.0 (pyramid)"
	case "server_encoding":
		return "UTF8"
	case "client_encoding":
		return "UTF8"
	case "transaction_isolation":
		return "read committed"
	case "standard_conforming_strings":
		return "on"
	case "integer_datetimes":
		return "on"
	case "extra_float_digits":
		return "3"
	case "datestyle":
		return "ISO, MDY"
	case "timezone":
		return "UTC"
	case "search_path":
		return "public"
	case "application_name":
		return "pyramid"
	case "is_superuser":
		return "on"
	case "session_authorization":
		return "pyramid"
	case "client_min_messages":
		return "notice"
	case "intervalstyle":
		return "postgres"
	case "integer":
		return "on"
	case "lock_timeout":
		return "0"
	case "max_connections":
		return "100"
	case "max_identifier_length":
		return "63"
	case "max_index_keys":
		return "32"
	case "row_security":
		return "off"
	default:
		return ""
	}
}

// execLayerSelect handles `SELECT ... FROM <layer> WHERE ... LIMIT ...`.
func (s *Server) execLayerSelect(be *pgproto3.Backend, sel *tree.Select, clause *tree.SelectClause, layerName string) {
	layer := s.LayerByName(layerName)

	// Decide target columns
	selectTarget := buildTarget(clause.Exprs)
	wantCount := selectTarget.kind == targetCount

	if wantCount {
		be.Send(rowDescFromColumns([]columnInfo{{Name: "count", OID: colOID(Int8)}}))
		if layer == nil || layer.Store == nil {
			be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("0")}})
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
			return
		}
		f := buildFilter(clause.Where)
		n, _ := layer.Store.CountEvents(f)
		be.Send(&pgproto3.DataRow{Values: [][]byte{[]byte(strconv.FormatUint(uint64(n), 10))}})
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		return
	}

	cols := selectTarget.columns
	if len(cols) == 0 {
		cols = eventColumnsToColumnInfo()
	}
	be.Send(rowDescFromColumns(cols))

	limit := limitOf(sel)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	if layer == nil || layer.Store == nil {
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 0")})
		return
	}

	f := buildFilter(clause.Where)
	if f.Limit > 0 && f.Limit < limit {
		limit = f.Limit
	}

	n := 0
	for evt := range layer.Store.QueryEvents(f, limit) {
		row := formatEventRow(evt)
		vals := make([][]byte, len(cols))
		for i, col := range cols {
			vals[i] = pickEventField(row, col.Name)
		}
		be.Send(&pgproto3.DataRow{Values: vals})
		n++
	}

	be.Send(&pgproto3.CommandComplete{CommandTag: []byte(fmt.Sprintf("SELECT %d", n))})
}

// eventColumnsToColumnInfo returns the default [eventColumns] layout.
func eventColumnsToColumnInfo() []columnInfo {
	out := make([]columnInfo, 0, len(eventColumns))
	for _, c := range eventColumns {
		out = append(out, columnInfo{Name: c.Name, OID: colOID(c.Type)})
	}
	return out
}

// pickEventField extracts the requested column from a row produced by
// [formatEventRow].
func pickEventField(row [][]byte, name string) []byte {
	if len(row) != len(eventColumns) {
		return nil
	}
	for i, c := range eventColumns {
		if c.Name == name {
			return row[i]
		}
	}
	return nil
}

// formatEventRow turns a nostr.Event into a [][]byte aligned to [eventColumns].
func formatEventRow(evt nostr.Event) [][]byte {
	tagsJSON := "["
	for i, tag := range evt.Tags {
		if i > 0 {
			tagsJSON += ","
		}
		tagsJSON += "["
		for j, v := range tag {
			if j > 0 {
				tagsJSON += ","
			}
			tagsJSON += strconv.Quote(v)
		}
		tagsJSON += "]"
	}
	tagsJSON += "]"
	return [][]byte{
		[]byte(evt.ID.Hex()),
		[]byte(evt.PubKey.Hex()),
		[]byte(strconv.Itoa(int(evt.Kind))),
		[]byte(strconv.FormatInt(int64(evt.CreatedAt), 10)),
		[]byte(evt.Content),
		[]byte(tagsJSON),
		[]byte(hex.EncodeToString(evt.Sig[:])),
	}
}

// --- scalar expression evaluation ---------------------------------------------

// evalScalarExpr tries hard to produce a (column name, value) pair for a
// top-level SELECT expression with no FROM. It recognizes constant literals,
// version(), current_database(), current_schema(), current_user and a couple
// of pg_catalog functions psql sends at startup.
func (s *Server) evalScalarExpr(e tree.Expr) (string, string) {
	switch ex := e.(type) {
	case *tree.UnresolvedName:
		if ex.NumParts >= 1 {
			nm := ex.Parts[0]
			switch nm {
			case "version":
				return "version", lookupSetting("server_version")
			case "current_database", "current_catalog":
				return nm, "pyramid"
			case "current_schema", "current_schemas":
				return nm, "public"
			case "current_user", "session_user", "user":
				return nm, "pyramid"
			case "now":
				return nm, ""
			}
			return nm, ""
		}
	case *tree.FuncExpr:
		fn := ""
		switch ref := ex.Func.FunctionReference.(type) {
		case *tree.UnresolvedName:
			fn = strings.ToLower(ref.Parts[0])
		}
		switch fn {
		case "version":
			return "version", lookupSetting("server_version")
		case "current_database":
			return "current_database", "pyramid"
		case "current_schema":
			return "current_schema", "public"
		case "current_user", "session_user":
			return fn, "pyramid"
		case "pg_catalog":
			// pg_catalog.pg_get_userbyid etc. — rare here; default empty
			return fn, ""
		case "now":
			return fn, ""
		default:
			// Best-effort: pick the first string literal argument, which makes
			// some clients happy, otherwise return empty.
			return fn, ""
		}
	case *tree.NumVal:
		name, val := nvalString(ex)
		return name, val
	case *tree.DString:
		return "?column?", string(*ex)
	case *tree.DBool:
		if bool(*ex) {
			return "?column?", "t"
		}
		return "?column?", "f"
	}

	// fall back to rendering via tree.AsString — gives e.g. NULL or expr text
	return "?column?", tree.AsString(e)
}

func nvalString(n *tree.NumVal) (string, string) {
	if n == nil {
		return "?column?", ""
	}
	if v, err := n.AsInt64(); err == nil {
		return "int4", strconv.FormatInt(v, 10)
	}
	return "float8", n.ExactString()
}

// sendEmpty emits a single-row-less result: a RowDescription matching the
// target list if we can read it, then no DataRows and a CommandComplete tag.
func (s *Server) sendEmpty(be *pgproto3.Backend) {
	be.Send(rowDescFromColumns([]columnInfo{{Name: "?column?", OID: colOID(Text)}}))
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 0")})
}

// hexstr is a small helper that mirrors the legacy hex.EncodeToString used in
// the old pg.go, kept available for catalog handlers that want to dump blobs.
func hexstr(b []byte) string { return hex.EncodeToString(b) }
