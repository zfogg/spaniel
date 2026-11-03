package ingestion

import sequel "github.com/zfogg/sequel/go"

// fingerprintSQL retains the internal helper for generic SQL callers. Invalid
// SQL has no fingerprint; raw SQL must never become a persisted identifier.
func fingerprintSQL(stmt string) string {
	fp, err := sequel.Fingerprint(sequel.DialectGeneric, stmt)
	if err != nil {
		return ""
	}
	return fp
}
