package querydiagnostics

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	sequel "github.com/zfogg/sequel/go"
)

// ValidateAttribute checks diagnostic wire structure before normalization. Unlike
// Normalize it never replaces invalid diagnostics with a fresh local analysis.
func ValidateAttribute(attrs map[string]any) bool {
	_, err := DecodeAttribute(attrs)
	return err == nil
}

// DecodeAttribute validates version and original length metadata and replaces
// received prose with Sequel catalog descriptions. Legacy senders default to v1
// and unknown source length; that never authorizes applying ranges to SQL.
func DecodeAttribute(attrs map[string]any) ([]sequel.Diagnostic, error) {
	if stringAttr(attrs, "spaniel.db.query.diagnostics_status") == "invalid" {
		return nil, errors.New("rejected SQL diagnostic metadata")
	}
	results, ok := decode(attrs[Attribute], attrs)
	if !ok {
		return nil, errors.New("invalid SQL diagnostic attribute")
	}
	return results, nil
}

func diagnosticMetadata(attrs map[string]any) (int, int, bool) {
	version, length := sequel.DiagnosticSchemaVersion, sequel.MaxInputBytes
	if v, ok := attrs["gotel.db.query.diagnostic_schema_version"]; ok {
		var valid bool
		version, valid = integer(v)
		if !valid || version != sequel.DiagnosticSchemaVersion {
			return 0, 0, false
		}
	}
	if v, ok := attrs["gotel.db.query.original_length"]; ok {
		var valid bool
		length, valid = integer(v)
		if !valid || length < 0 {
			return 0, 0, false
		}
	}
	return version, length, true
}

func integer(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		if int64(int(n)) == n {
			return int(n), true
		}
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 9007199254740991 && n == math.Trunc(n) {
			return int(n), true
		}
	case json.Number:
		i, err := strconv.Atoi(string(n))
		return i, err == nil
	}
	return 0, false
}
