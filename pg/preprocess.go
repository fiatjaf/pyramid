package pg

import (
	"regexp"
	"strings"
)

// preprocessSQL rewrites common PG syntax the pure-Go parser doesn't accept
// into forms it does. Specifically:
//
//   - `OPERATOR(pg_catalog.OP)` is rewritten to bare `OP` so the auxten parser
//     (cockroach-era) accepts what psql emits for some schema introspection
//     queries (eg. \d <tbl>).
//   - `COLLATE pg_catalog.X` is dropped (no collation support here).
//
// Anything we can't recognize is left untouched, so this is purely syntactic
// sugar — semantically equivalent for our limited executor.
//
// psql 18's \d command emits both constructs.
func preprocessSQL(sql string) string {
	sql = operatorRe.ReplaceAllStringFunc(sql, operatorReplace)
	sql = collateRe.ReplaceAllString(sql, "")
	sql = castSchemaRe.ReplaceAllString(sql, "::$1")
	return sql
}

func operatorReplace(match string) string {
	s := operatorInnerRE.FindStringSubmatch(match)
	if s == nil || len(s) < 3 {
		return match
	}
	op := strings.TrimSpace(s[2])
	if i := strings.IndexByte(op, '.'); i >= 0 {
		op = op[i+1:]
	}
	if op == "" {
		return match
	}
	return op
}

var (
	operatorRe      = regexp.MustCompile(`OPERATOR\([^)]*\)`)
	operatorInnerRE = regexp.MustCompile(`OPERATOR\(([^.]*)(?:\.([^)]+))?\)`)
	collateRe       = regexp.MustCompile(`(?i)COLLATE\s+pg_catalog\.[A-Za-z0-9_]+`)
	castSchemaRe    = regexp.MustCompile(`(?i)::\s*pg_catalog\.([A-Za-z0-9_]+)`)
)
