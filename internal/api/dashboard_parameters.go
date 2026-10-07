package api

import "strings"

// usesSessionParameter recognizes the reserved DuckDB bind parameter without
// mistaking quoted text, identifiers, dollar strings, or comments for a bind.
func usesSessionParameter(query string) bool {
	return usesNamedParameter(query, "session_id")
}

func usesNamedParameter(query, name string) bool {
	for i := 0; i < len(query); {
		switch {
		case strings.HasPrefix(query[i:], "--"):
			for i < len(query) && query[i] != '\n' {
				i++
			}
		case strings.HasPrefix(query[i:], "/*"):
			i += 2
			for depth := 1; i < len(query) && depth > 0; {
				if strings.HasPrefix(query[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(query[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
		case query[i] == '\'' || query[i] == '"':
			quote := query[i]
			escaped := quote == '\'' && i > 0 && (query[i-1] == 'E' || query[i-1] == 'e')
			i++
			for i < len(query) {
				if query[i] == quote {
					i++
					if i < len(query) && query[i] == quote {
						i++
						continue
					}
					break
				}
				if escaped && query[i] == '\\' && i+1 < len(query) {
					i++
				}
				i++
			}
		case query[i] == '$':
			start := i
			i++
			for i < len(query) && parameterChar(query[i]) {
				i++
			}
			if i < len(query) && query[i] == '$' {
				delimiter := query[start : i+1]
				i++
				if end := strings.Index(query[i:], delimiter); end >= 0 {
					i += end + len(delimiter)
				} else {
					return false
				}
			} else if strings.EqualFold(query[start:i], "$"+name) {
				return true
			}
		default:
			i++
		}
	}
	return false
}

func parameterChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}
