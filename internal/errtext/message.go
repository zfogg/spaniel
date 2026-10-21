// Package errtext bounds externally displayed diagnostics without losing the
// innermost cause of recursively wrapped DuckDB invalidation errors.
package errtext

import "strings"

const Limit = 4096
const invalidated = "database has been invalidated because of a previous fatal error"

func Message(message string) string {
	if strings.Contains(message, invalidated) {
		cause := message
		if i := strings.LastIndex(message, "Original error: "); i >= 0 {
			cause = message[i+len("Original error: "):]
		}
		cause = strings.Trim(cause, "\" \n\r\t")
		if strings.Contains(cause, invalidated) {
			cause = "The original cause was not supplied."
		}
		message = "Database unavailable after a fatal DuckDB error; the collector must be restarted. Original cause: " + cause
	}
	if len(message) > Limit {
		message = strings.ToValidUTF8(message[:Limit], "") + "… [truncated]"
	}
	return message
}
