package linter

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
)

var dynamicPathSegment = regexp.MustCompile(`/(?:[0-9]+|[0-9a-fA-F]{8}-[0-9a-fA-F-]{27,})(?:/|$)`)

type LintRule struct {
	ID       string
	Severity string
	Check    func(s *storage.Span) (string, bool)
}

var rules = []LintRule{
	{
		ID: "span.missing_application_prefix", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if isProtocolSpan(s) {
				return "", false
			}
			prefix := strings.SplitN(s.ServiceName, "-", 2)[0]
			if prefix == "" || prefix == "unknown_service" || strings.HasPrefix(s.Name, prefix+".") {
				return "", false
			}
			return "span name is not prefixed with application name '" + prefix + ".'", true
		},
	},
	{
		ID: "span.high_cardinality_path", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			path := firstAttr(s.Attributes, "http.route", "url.path", "http.target")
			if dynamicPathSegment.MatchString(path) {
				return "span contains a raw numeric or UUID path segment; use a route template", true
			}
			return "", false
		},
	},
	{
		ID: "http.client.missing_destination", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.Kind == 3 && IsHTTPSpan(s) && firstAttr(s.Attributes, "server.address", "net.peer.name", "http.host") == "" {
				return "HTTP client span missing destination host", true
			}
			return "", false
		},
	},
	{
		ID: "http.server.raw_url_name", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if s.Kind == 2 && IsHTTPSpan(s) && (strings.Contains(s.Name, "://") || strings.ContainsAny(s.Name, "?#")) {
				return "HTTP server span name contains a raw URL, query, or fragment", true
			}
			return "", false
		},
	},
	{
		ID: "redis.nonstandard_name", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if strings.EqualFold(firstAttr(s.Attributes, "db.system.name", "db.system"), "redis") {
				prefix := strings.SplitN(s.ServiceName, "-", 2)[0] + ".storage.redis."
				if !strings.HasPrefix(strings.ToLower(s.Name), strings.ToLower(prefix)) {
					return "Redis span name must use " + prefix + "<command>", true
				}
			}
			return "", false
		},
	},
	{
		ID: "temporal.nonstandard_name", Severity: "warning",
		Check: func(s *storage.Span) (string, bool) {
			if hasAttr(s.Attributes, "temporalWorkflowType") || hasAttr(s.Attributes, "temporalActivityType") {
				prefix := strings.SplitN(s.ServiceName, "-", 2)[0] + ".temporal."
				if !strings.HasPrefix(s.Name, prefix+"workflow.") && !strings.HasPrefix(s.Name, prefix+"activity.") && !strings.HasPrefix(s.Name, prefix+"workflow.child.") {
					return "Temporal span name must identify the application and workflow/activity type", true
				}
			}
			return "", false
		},
	},
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
			system := firstAttr(s.Attributes, "db.system.name", "db.system")
			if system == "" {
				return "", false
			}
			// Redis protocol operations are fully described by db.operation.name;
			// unlike SQL database spans, they do not carry db.query.text.
			if strings.EqualFold(system, "redis") {
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

func firstAttr(attrs string, keys ...string) string {
	var values map[string]any
	if json.Unmarshal([]byte(attrs), &values) != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
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

// isProtocolSpan identifies spans whose names are defined by an OpenTelemetry
// protocol or instrumentation library rather than application code.
func isProtocolSpan(s *storage.Span) bool {
	if IsHTTPSpan(s) {
		return true
	}
	for _, key := range []string{
		"db.system.name", "db.system",
		"rpc.system", "rpc.service", "rpc.method",
		"messaging.system",
		"temporalWorkflowType", "temporalActivityType",
	} {
		if hasAttr(s.Attributes, key) {
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
