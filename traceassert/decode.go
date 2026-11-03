package traceassert

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/collector/pdata/ptrace"
)

const maxCapture = 64 << 20

// Span is the common representation for normalized Spaniel and OTLP captures.
type Span struct {
	// TraceID is a nonzero 32-character lowercase hexadecimal trace identifier.
	TraceID string `json:"trace_id"`
	// SpanID is a nonzero 16-character lowercase hexadecimal span identifier.
	SpanID string `json:"span_id"`
	// ParentSpanID names a parent in this trace; empty means a root.
	ParentSpanID string `json:"parent_span_id"`
	// ServiceName comes from resource service.name.
	ServiceName string `json:"service_name"`
	// Name is the original operation name; it is never echoed in reports.
	Name string `json:"name"`
	// Kind uses OTLP span kind integers.
	Kind int `json:"kind"`
	// StatusCode uses OTLP status numbering: unset 0, OK 1, error 2.
	StatusCode int `json:"status_code"`
	// StatusMessage preserves the description for redaction checks; it is never
	// copied into an assertion report or a decode error.
	StatusMessage string `json:"status_message,omitempty"`
	// Attributes preserves typed SDK values or decoded JSON values.
	Attributes map[string]any `json:"attributes"`
	// Resource contains the resource attribute map.
	Resource map[string]any `json:"resource,omitempty"`
	// Events contains event metadata, including nested attributes.
	Events []any `json:"events,omitempty"`
	// Links contains outbound causal links.
	Links []Link `json:"links,omitempty"`
}

// Link names a causal span, optionally in another trace.
type Link struct {
	// TraceID names the linked ancestor trace.
	TraceID string `json:"trace_id"`
	// SpanID names the linked ancestor span.
	SpanID string `json:"span_id"`
	// Attributes preserves typed SDK values or decoded JSON values.
	Attributes map[string]any `json:"attributes,omitempty"`
}

func (s Span) key() string { return s.TraceID + "/" + s.SpanID }
func validID(s string, n int) bool {
	if len(s) != n || strings.ToLower(s) != s {
		return false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

// Decode reads a bounded capture. Format is json (auto-detect normalized or OTLP),
// otlp-json or otlp-proto. Malformed input errors never include captured values.
func Decode(r io.Reader, format string) ([]Span, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCapture+1))
	if err != nil || len(data) > maxCapture {
		return nil, fmt.Errorf("capture unreadable or exceeds 64 MiB")
	}
	var t ptrace.Traces
	if format == "otlp-proto" {
		t, err = (&ptrace.ProtoUnmarshaler{}).UnmarshalTraces(data)
	} else if format == "otlp-json" {
		t, err = (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(data)
	} else if format == "json" || format == "" {
		var env map[string]json.RawMessage
		if json.Unmarshal(data, &env) == nil {
			if _, ok := env["resourceSpans"]; ok {
				t, err = (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(data)
			} else {
				if spans, ok := env["spans"]; ok {
					data = spans
				} else if nested, ok := env["data"]; ok {
					return Decode(strings.NewReader(string(nested)), "json")
				} else {
					return nil, fmt.Errorf("capture has no spans")
				}
			}
		}
		if t == (ptrace.Traces{}) {
			return decodeNormalized(data)
		}
	} else {
		return nil, fmt.Errorf("unsupported capture format")
	}
	if err != nil {
		return nil, fmt.Errorf("invalid OTLP capture")
	}
	out := []Span{}
	for _, rs := range t.ResourceSpans().All() {
		resource := rs.Resource().Attributes().AsRaw()
		service, _ := resource["service.name"].(string)
		for _, ss := range rs.ScopeSpans().All() {
			for _, s := range ss.Spans().All() {
				span := Span{TraceID: s.TraceID().String(), SpanID: s.SpanID().String(), ServiceName: service, Name: s.Name(), Kind: int(s.Kind()), StatusCode: int(s.Status().Code()), StatusMessage: s.Status().Message(), Attributes: s.Attributes().AsRaw(), Resource: resource}
				if !s.ParentSpanID().IsEmpty() {
					span.ParentSpanID = s.ParentSpanID().String()
				}
				for _, e := range s.Events().All() {
					span.Events = append(span.Events, map[string]any{"name": e.Name(), "attributes": e.Attributes().AsRaw()})
				}
				for _, l := range s.Links().All() {
					span.Links = append(span.Links, Link{l.TraceID().String(), l.SpanID().String(), l.Attributes().AsRaw()})
				}
				out = append(out, span)
			}
		}
	}
	return out, nil
}

func decodeNormalized(data []byte) ([]Span, error) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(data, &rows) != nil || rows == nil {
		return nil, fmt.Errorf("invalid normalized capture")
	}
	out := make([]Span, 0, len(rows))
	for _, row := range rows {
		if err := decodeAttributeObjects(row); err != nil {
			return nil, err
		}
		for _, key := range []string{"events", "links"} {
			raw, ok := row[key]
			if !ok || string(raw) == "null" {
				continue
			}
			var entries []map[string]json.RawMessage
			if json.Unmarshal(raw, &entries) != nil {
				return nil, fmt.Errorf("invalid metadata collection")
			}
			for _, entry := range entries {
				if err := decodeAttributeObjects(entry); err != nil {
					return nil, err
				}
				// Spaniel links include the source trace/span plus separate
				// linked IDs. The verifier's Link always names the target.
				if key == "links" {
					if target, ok := entry["linked_trace_id"]; ok {
						entry["trace_id"] = target
					}
					if target, ok := entry["linked_span_id"]; ok {
						entry["span_id"] = target
					}
				}
			}
			encoded, err := json.Marshal(entries)
			if err != nil {
				return nil, fmt.Errorf("invalid metadata")
			}
			row[key] = encoded
		}
		b, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("invalid normalized span")
		}
		var span Span
		if json.Unmarshal(b, &span) != nil {
			return nil, fmt.Errorf("invalid normalized span")
		}
		if span.Attributes == nil {
			span.Attributes = map[string]any{}
		}
		out = append(out, span)
	}
	return out, nil
}

func decodeAttributeObjects(row map[string]json.RawMessage) error {
	for _, key := range []string{"attributes", "resource"} {
		raw, ok := row[key]
		if !ok {
			continue
		}
		var encoded string
		if json.Unmarshal(raw, &encoded) == nil {
			var object map[string]any
			if json.Unmarshal([]byte(encoded), &object) != nil || object == nil {
				return fmt.Errorf("invalid encoded attribute object")
			}
			row[key] = json.RawMessage(encoded)
		}
	}
	return nil
}

// DecodeFindings accepts a normalized finding array or Spaniel data envelope.
func DecodeFindings(r io.Reader) ([]Finding, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCapture+1))
	if err != nil || len(data) > maxCapture {
		return nil, fmt.Errorf("findings unreadable or too large")
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &env) == nil && env.Data != nil {
		data = env.Data
	}
	var findings []Finding
	if json.Unmarshal(data, &findings) != nil || findings == nil {
		return nil, fmt.Errorf("invalid findings capture")
	}
	for _, f := range findings {
		if f.RuleID == "" || f.SpanID == "" {
			return nil, fmt.Errorf("incomplete finding")
		}
	}
	return findings, nil
}

// FetchTrace fetches one complete trace through Spaniel's read-only trace API.
// The caller controls authentication via its http.Client transport.
func FetchTrace(ctx context.Context, client *http.Client, baseURL, traceID string) ([]Span, error) {
	if !validID(traceID, 32) {
		return nil, fmt.Errorf("invalid trace ID")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/traces/"+traceID, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid collector URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trace request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trace request returned HTTP %d", resp.StatusCode)
	}
	return Decode(resp.Body, "json")
}
