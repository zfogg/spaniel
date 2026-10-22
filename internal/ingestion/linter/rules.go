package linter

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
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
			if !IsHTTPSpan(s) {
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
			if !IsHTTPSpan(s) {
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
			if !hasAttr(s.Attributes, "db.query.text") && !hasAttr(s.Attributes, "db.statement") &&
				!hasAttr(s.Attributes, "db.operation.name") && !hasAttr(s.Attributes, "db.operation") {
				return "", false
			}
			if !hasAttr(s.Attributes, "db.system.name") && !hasAttr(s.Attributes, "db.system") {
				return "DB span missing db.system.name attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "db.missing_statement",
		Severity: "info",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "db.system.name") && !hasAttr(s.Attributes, "db.system") {
				return "", false
			}
			if !hasAttr(s.Attributes, "db.query.text") && !hasAttr(s.Attributes, "db.statement") {
				return "DB span missing db.query.text attribute", true
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
	{
		ID:       "rpc.missing_system",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "rpc.service") && !hasAttr(s.Attributes, "rpc.method") {
				return "", false
			}
			if !hasAttr(s.Attributes, "rpc.system") {
				return "RPC span missing required rpc.system attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "rpc.missing_method",
		Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "rpc.system") {
				return "", false
			}
			if !hasAttr(s.Attributes, "rpc.method") {
				return "RPC span missing required rpc.method attribute", true
			}
			return "", false
		},
	},
	{
		ID:       "rpc.missing_service",
		Severity: "info",
		Check: func(s *storage.Span) (string, bool) {
			if !hasAttr(s.Attributes, "rpc.system") {
				return "", false
			}
			if !hasAttr(s.Attributes, "rpc.service") {
				return "RPC span missing recommended rpc.service attribute", true
			}
			return "", false
		},
	},
}

// Span kind describes the transport role, not its protocol. SQL clients and
// message producers must not be asked to invent HTTP attributes.
// IsHTTPSpan reports whether a span carries HTTP semantics.
func IsHTTPSpan(s *storage.Span) bool {
	if s.Kind != 2 && s.Kind != 3 {
		return false
	} // OTLP server=2, client=3
	for _, key := range []string{"http.request.method", "http.method", "http.response.status_code", "http.status_code", "http.route", "http.url", "url.full", "url.path"} {
		if hasAttr(s.Attributes, key) {
			return true
		}
	}
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE"} {
		if s.Name == method || strings.HasPrefix(s.Name, method+" ") {
			return true
		}
	}
	return false
}

// LintSpan runs the semantic-convention lint rules for a persisted span.
func LintSpan(s *storage.Span, sessionID string, store *storage.DB, tracer trace.Tracer) {
	_, lspan := tracer.Start(context.Background(), "lintSpan",
		trace.WithAttributes(
			attribute.String("span_id", s.SpanID),
			attribute.String("service.name", s.ServiceName),
		))
	defer lspan.End()
	for _, rule := range rules {
		msg, fired := rule.Check(s)
		if !fired {
			continue
		}
		telemetry.Catalog().RecordLintWarning(context.Background(), rule.ID, rule.Severity)
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
