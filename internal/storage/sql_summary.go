package storage

import (
	"context"
	"strconv"
	"strings"

	"github.com/sqlc-dev/darkwing/ast"
	"github.com/sqlc-dev/darkwing/parser"
)

// queryNameKey carries an optional, application-owned query name. It is
// intentionally private so only this package controls how it is read.
type queryNameKey struct{}

// WithQueryName attaches a stable, human-owned name to one database operation.
// Empty names are ignored. It is intended for callers of ReadOnlyQuery when a
// user has supplied a meaningful name for their SQL.
func WithQueryName(ctx context.Context, name string) context.Context {
	if strings.TrimSpace(name) == "" {
		return ctx
	}
	return context.WithValue(ctx, queryNameKey{}, strings.TrimSpace(name))
}

func queryNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(queryNameKey{}).(string)
	return name
}

// sqlSummary returns a parameter-free description of one DuckDB statement.
// User SELECT names retain a bounded query shape (projection, source,
// predicates, grouping, and ordering), but never literal values.
func sqlSummary(query string) string {
	stmt, err := parser.ParseStatement(context.Background(), query)
	if err != nil {
		return "SQL"
	}
	if _, ok := stmt.(*ast.SelectStatement); ok {
		if sanitized, ok := sanitizeSQL(query); ok {
			if summary, ok := selectSummary(sanitized); ok {
				return summary
			}
		}
	}
	return statementSummary(stmt)
}

// selectSummary keeps the useful structural clauses of a top-level SELECT
// while bounding each clause so user-authored SQL cannot create unbounded span
// names. sanitizeSQL has already replaced all literal values with ?.
func selectSummary(sanitized string) (string, bool) {
	keywords := topLevelSQLKeywords(sanitized)
	selectAt, foundSelect := keywords["SELECT"]
	fromAt, foundFrom := keywords["FROM"]
	if !foundSelect || foundFrom && fromAt <= selectAt {
		return "", false
	}
	clause := func(start int, endKeys ...string) string {
		end := len(sanitized)
		for _, key := range endKeys {
			if at, ok := keywords[key]; ok && at > start && at < end {
				end = at
			}
		}
		return strings.TrimSpace(sanitized[start:end])
	}
	parts := []string{shortenSQL(clause(selectAt, "FROM", "WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"), 64)}
	if foundFrom {
		parts = append(parts, shortenSQL(clause(fromAt, "WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"), 48))
	}
	for _, key := range []string{"WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"} {
		if at, ok := keywords[key]; ok {
			parts = append(parts, shortenSQL(clause(at, followingSQLClauses(key)...), 64))
		}
	}
	return strings.Join(parts, " ") + " · " + strconv.Itoa(countSQLArgs(sanitized)) + " args", true
}

func followingSQLClauses(key string) []string {
	clauses := []string{"WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET"}
	for i, clause := range clauses {
		if clause == key {
			return clauses[i+1:]
		}
	}
	return nil
}

func topLevelSQLKeywords(s string) map[string]int {
	keywords := map[string]int{}
	for i, depth := 0, 0; i < len(s); {
		switch s[i] {
		case '\'', '"', '`':
			i = skipQuoted(s, i, s[i])
			continue
		case '(':
			depth++
			i++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth == 0 {
			for _, key := range []string{"GROUP BY", "ORDER BY", "SELECT", "FROM", "WHERE", "HAVING", "LIMIT", "OFFSET"} {
				if _, seen := keywords[key]; !seen && sqlKeywordAt(s, i, key) {
					keywords[key] = i
					i += len(key)
					goto next
				}
			}
		}
		i++
	next:
	}
	return keywords
}

func sqlKeywordAt(s string, start int, key string) bool {
	end := start + len(key)
	if end > len(s) || !strings.EqualFold(s[start:end], key) {
		return false
	}
	isWord := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	return (start == 0 || !isWord(s[start-1])) && (end == len(s) || !isWord(s[end]))
}

func shortenSQL(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max-1]) + "…"
}

func countSQLArgs(s string) int {
	count := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' || s[i] == '"' || s[i] == '`' {
			i = skipQuoted(s, i, s[i]) - 1
			continue
		}
		if s[i] == '?' {
			count++
		}
	}
	return count
}

func statementSummary(stmt ast.Stmt) string {
	switch s := stmt.(type) {
	case *ast.SelectStatement:
		return withTarget("SELECT", selectTarget(s))
	case *ast.InsertStatement:
		return withTarget("INSERT", s.Table)
	case *ast.UpdateStatement:
		return withTarget("UPDATE", tableTarget(s.Table))
	case *ast.DeleteStatement:
		return withTarget("DELETE", tableTarget(s.Table))
	case *ast.MergeIntoStatement:
		return withTarget("MERGE", tableTarget(s.Target))
	case *ast.ExplainStatement:
		return "EXPLAIN " + statementSummary(s.Statement)
	default:
		return "SQL"
	}
}

func withTarget(operation, target string) string {
	if target == "" {
		return operation
	}
	return operation + " " + target
}

func selectTarget(stmt *ast.SelectStatement) string { return queryNodeTarget(stmt.Node) }

func queryNodeTarget(node ast.QueryNode) string {
	switch n := node.(type) {
	case *ast.SelectNode:
		return tableTarget(n.FromTable)
	case *ast.SetOperationNode:
		if len(n.Inputs) > 0 {
			return queryNodeTarget(n.Inputs[0])
		}
	}
	return ""
}

func tableTarget(table ast.TableRef) string {
	switch t := table.(type) {
	case *ast.BaseTableRef:
		return t.TableName
	case *ast.JoinRef:
		return tableTarget(t.Left)
	case *ast.SubqueryRef:
		return selectTarget(t.Subquery)
	}
	return ""
}

func skipQuoted(query string, start int, quote byte) int {
	i := start + 1
	for i < len(query) {
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(query)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// sanitizeSQL replaces literals with placeholders before query text is added
// to telemetry. It returns false for an unterminated literal, in which case
// callers should omit db.query.text rather than risk recording sensitive data.
func sanitizeSQL(query string) (string, bool) {
	var out strings.Builder
	space := false
	for i := 0; i < len(query); {
		if isSpace(query[i]) {
			space = true
			i++
			continue
		}
		if i+1 < len(query) && (query[i:i+2] == "--" || query[i:i+2] == "/*") {
			if query[i+1] == '-' {
				i += 2
				for i < len(query) && query[i] != '\n' {
					i++
				}
			} else {
				end := strings.Index(query[i+2:], "*/")
				if end < 0 {
					return "", false
				}
				i += end + 4
			}
			space = true
			continue
		}
		if space && out.Len() > 0 {
			out.WriteByte(' ')
		}
		space = false
		if query[i] == '\'' {
			end := skipQuoted(query, i, '\'')
			if end == len(query) && (len(query) < 2 || query[len(query)-1] != '\'') {
				return "", false
			}
			out.WriteByte('?')
			i = end
			continue
		}
		if query[i] >= '0' && query[i] <= '9' {
			for i < len(query) && ((query[i] >= '0' && query[i] <= '9') || strings.ContainsRune(".eE+-", rune(query[i]))) {
				i++
			}
			out.WriteByte('?')
			continue
		}
		if query[i] == '$' && i+1 < len(query) && query[i+1] >= '0' && query[i+1] <= '9' {
			i += 2
			for i < len(query) && query[i] >= '0' && query[i] <= '9' {
				i++
			}
			out.WriteByte('?')
			continue
		}
		out.WriteByte(query[i])
		i++
	}
	return strings.TrimSpace(out.String()), true
}
