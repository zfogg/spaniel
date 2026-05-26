package ingestion

import (
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

type LintRule struct {
	ID       string
	Severity string
	Check    func(s *storage.Span) (string, bool)
}

var rules = []LintRule{
	{
		ID:       "http.missing_method",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.Kind != 3 && s.Kind != 4 { // client=3, server=4
				return "", false
			}
			if !hasAttr(s.Attributes, "http.request.method") && !hasAttr(s.Attributes, "http.method") {
				return "HTTP span missing http.request.method attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "http.missing_status_code",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.Kind != 3 && s.Kind != 4 {
				return "", false
			}
			if !hasAttr(s.Attributes, "http.response.status_code") && !hasAttr(s.Attributes, "http.status_code") {
				return "HTTP span missing http.response.status_code attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "db.missing_system",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "db.statement") && !hasAttr(s.Attributes, "db.operation") {
				return "", false
			}
			if !hasAttr(s.Attributes, "db.system") {
				return "DB span missing db.system attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "db.missing_statement",
		Severity: "info",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "db.system") {
				return "", false
			}
			if !hasAttr(s.Attributes, "db.statement") {
				return "DB span missing db.statement attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "unknown_service",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.ServiceName == "unknown_service" || s.ServiceName == "" {
				return "service.name is 'unknown_service' — set OTEL_SERVICE_NAME", true
			}
			return "", false
		},
	},
	{
		ID:       "zero_duration",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.EndNs > 0 && s.EndNs == s.StartNs {
				return "span has zero duration", true
			}
			return "", false
		},
	},
}

func lintSpan(s *storage.Span, sessionID string, store *storage.DB) {
	for _, rule := range rules {
		msg, fired := rule.Check(s)
		if !fired {
			continue
		}
		_ = store.InsertLintWarning(&storage.LintWarning{
			SpanID:    s.SpanID,
			TraceID:   s.TraceID,
			SessionID: sessionID,
			RuleID:    rule.ID,
			Message:   msg,
			Severity:  rule.Severity,
			CreatedAt: time.Now().UnixNano(),
		})
	}
}

func hasAttr(attrsJSON string, key string) bool {
	if len(attrsJSON) < 2 {
		return false
	}
	// simple substring check is sufficient for attribute key presence
	return contains(attrsJSON, `"`+key+`"`)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
