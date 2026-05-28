package ingestion

import (
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

// fingerprintSQL normalises a SQL statement by replacing all literal values
// with ? using the Vitess SQL parser. Falls back to raw statement on parse error.
func fingerprintSQL(stmt string) string {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return fallbackFingerprint(stmt)
	}
	parsed, err := parser.Parse(stmt)
	if err != nil {
		return fallbackFingerprint(stmt)
	}
	buf := sqlparser.NewTrackedBuffer(func(buf *sqlparser.TrackedBuffer, node sqlparser.SQLNode) {
		switch node.(type) {
		case *sqlparser.Literal:
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
