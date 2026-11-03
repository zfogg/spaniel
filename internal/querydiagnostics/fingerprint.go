package querydiagnostics

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	sequel "github.com/zfogg/sequel/go"
)

// Fingerprint uses the producer's versioned fingerprint when available. Otherwise
// it analyzes available SQL with its dialect; failures never fall back to raw SQL.
func Fingerprint(attrs map[string]any) string {
	system := stringAttr(attrs, "db.system.name")
	if system == "" {
		system = stringAttr(attrs, "db.system")
	}
	dialect, sql := sqlDialect(system)
	if !sql {
		return ""
	}
	if fp := stringAttr(attrs, "gotel.db.query.fingerprint"); fp != "" {
		prefix := "sequel:v1:" + string(dialect) + ":"
		digest := strings.TrimPrefix(fp, prefix)
		if strings.HasPrefix(fp, prefix) && len(digest) == 64 && digest == strings.ToLower(digest) {
			if _, err := hex.DecodeString(digest); err == nil {
				return fp
			}
		}
		return ""
	}
	if truncated, _ := attrs["gotel.db.query.truncated"].(bool); truncated {
		return ""
	}
	query := stringAttr(attrs, "db.query.text")
	if query == "" {
		query = stringAttr(attrs, "db.statement")
	}
	if query == "" {
		return ""
	}
	fp, err := sequel.Fingerprint(dialect, query)
	if err != nil {
		return ""
	}
	return fp
}

// Identity scopes equal fingerprints by service and database/server identity.
func Identity(attrs map[string]any, service, fp string) string {
	first := func(keys ...string) any {
		for _, k := range keys {
			if v, ok := attrs[k]; ok && v != nil {
				return v
			}
		}
		return ""
	}
	values := []any{service, fp, first("db.namespace", "db.name"), first("server.address", "net.peer.name"), fmt.Sprint(first("server.port", "net.peer.port"))}
	data, _ := json.Marshal(values)
	return string(data)
}
