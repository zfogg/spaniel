package ingestion

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/storage"
)

var (
	lintWarningsCounter     metric.Int64Counter
	lintWarningsCounterOnce sync.Once
)

func getLintWarningsCounter() metric.Int64Counter {
	lintWarningsCounterOnce.Do(func() {
		lintWarningsCounter, _ = otel.Meter("spaniel/ingestion").Int64Counter(
			"spaniel.lint.warnings",
			metric.WithDescription("Lint warnings fired during span analysis"),
			metric.WithUnit("{warning}"),
		)
	})
	return lintWarningsCounter
}

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

func lintSpan(s *storage.Span, sessionID string, store *storage.DB, tracer trace.Tracer) {
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
		getLintWarningsCounter().Add(context.Background(), 1,
			metric.WithAttributes(
				attribute.String("rule_id", rule.ID),
				attribute.String("severity", rule.Severity),
			))
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
