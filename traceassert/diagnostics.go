package traceassert

import "github.com/zfogg/spaniel/internal/querydiagnostics"

func validDiagnostics(attrs map[string]any, value any) bool {
	return querydiagnostics.ValidateAttribute(attrs)
}

func diagnosticCodes(attrs map[string]any) ([]string, bool) {
	diagnostics, err := querydiagnostics.DecodeAttribute(attrs)
	if err != nil {
		return nil, false
	}
	codes := make([]string, 0, len(diagnostics))
	for _, d := range diagnostics {
		codes = append(codes, d.Code)
	}
	return codes, true
}

func normalizedDiagnostics(attrs map[string]any) bool {
	switch values := attrs["db.query.diagnostics"].(type) {
	case []string:
		return len(values) == 0
	case []any:
		for _, v := range values {
			if _, ok := v.(map[string]any); !ok {
				return false
			}
		}
		return true
	case []map[string]any:
		return true
	}
	return false
}
