package ingestion

import (
	"strings"

	"github.com/xwb1989/sqlparser"
)

// fingerprintSQL normalises a SQL statement by replacing all literal values
// with ? using a lightweight SQL parser. Falls back to whitespace
// normalisation on parse error (non-standard dialects, truncated statements,
// non-SQL input). The parser exposes package-level Parse, so there is no
// per-call parser construction.
func fingerprintSQL(stmt string) string {
	parsed, err := sqlparser.Parse(stmt)
	if err != nil {
		return fallbackFingerprint(stmt)
	}
	buf := sqlparser.NewTrackedBuffer(func(buf *sqlparser.TrackedBuffer, node sqlparser.SQLNode) {
		switch node.(type) {
		case *sqlparser.SQLVal:
			buf.WriteString("?")
		case sqlparser.ListArg:
			buf.WriteString("(?)")
		default:
			node.Format(buf)
		}
	})
	parsed.Format(buf)
	return strings.TrimSpace(buf.String())
}

// fallbackFingerprint uses whitespace normalisation when the Vitess parser
// fails (non-standard SQL dialects, truncated statements, etc.)
func fallbackFingerprint(stmt string) string {
	fields := strings.Fields(stmt)
	return strings.Join(fields, " ")
}
