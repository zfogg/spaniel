package traceassert

import (
	"context"
	"encoding/json"
	"fmt"
	sequel "github.com/zfogg/sequel/go"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const traceA = "00000000000000000000000000000001"
const traceB = "00000000000000000000000000000002"

func id(n int) string   { return fmt.Sprintf("%016x", n) }
func ptr[T any](v T) *T { return &v }
func fixture() []Span {
	return []Span{
		{TraceID: traceA, SpanID: id(1), ServiceName: "browser", Name: "click", Attributes: map[string]any{}},
		{TraceID: traceA, SpanID: id(2), ParentSpanID: id(1), ServiceName: "api", Name: "GET /", Kind: 2, Attributes: map[string]any{}},
		{TraceID: traceA, SpanID: id(3), ParentSpanID: id(2), ServiceName: "api", Name: "query", Kind: 3, Attributes: map[string]any{"db.system.name": "sqlite", "db.query.text": "SELECT ?", "db.query.diagnostics": []any{}}},
	}
}

func TestDiagnosticRulesCatalogCodesNormalizationAndOversizeMetadata(t *testing.T) {
	d := sequel.Diagnose(sequel.DialectPostgreSQL, "SELECT 'fixture-private")
	encoded, _ := json.Marshal(d)
	s := fixture()
	s[2].Attributes = map[string]any{"db.system.name": "postgresql", "gotel.db.query.text_reason": "malformed", "gotel.db.query.original_length": len("SELECT 'fixture-private"), "gotel.db.query.diagnostic_schema_version": 1, "db.query.diagnostics": []string{string(encoded)}}
	spec := Spec{Version: 1, Diagnostics: []DiagnosticRule{{Name: "malformed", Select: Selector{Name: "query"}, Codes: []string{"unterminated_string"}, TextReason: ptr("malformed")}}}
	if r := Check(s, spec); !r.OK() {
		t.Fatalf("SDK diagnostic rule: %+v", r)
	}
	spec.Diagnostics[0].NormalizedOnly = true
	if !hasFailure(Check(s, spec), "diagnostics_not_normalized") {
		t.Fatal("persisted SDK strings passed normalized-only rule")
	}
	var object map[string]any
	json.Unmarshal(encoded, &object)
	s[2].Attributes["db.query.diagnostics"] = []any{object}
	if r := Check(s, spec); !r.OK() {
		t.Fatalf("structured diagnostic rule: %+v", r)
	}
	spec.Diagnostics[0].Codes = []string{}
	if !hasFailure(Check(s, spec), "unexpected_diagnostic_codes") {
		t.Fatal("wrong expected code set passed")
	}
	// A limit diagnostic can report a supplied input length above the parser's
	// limit without allocating that input. Verification must retain the reason.
	for _, candidate := range sequel.DiagnosticCatalog() {
		if candidate.Code == "input_limit" {
			d = candidate
			break
		}
	}
	d.Offset, d.EndOffset, d.Line, d.Column, d.EndLine, d.EndColumn = 0, 0, 1, 1, 1, 1
	s[2].Attributes["db.query.diagnostics"] = []sequel.Diagnostic{d}
	s[2].Attributes["gotel.db.query.original_length"] = int64(sequel.MaxInputBytes) + 1
	spec = Spec{Version: 1, Redaction: Redaction{ValidateDiagnostics: true}}
	if r := Check(s, spec); !r.OK() {
		t.Fatalf("oversize metadata: %+v", r)
	}
}
func baseSpec() Spec {
	return Spec{Version: 1, NoDuplicateSpanIDs: true, Graph: Graph{true, true, true}, Ancestry: []Ancestry{{Name: "browser-api-db", Path: []Selector{{Service: "browser"}, {Service: "api", Kind: ptr(2)}, {Attributes: map[string]any{"db.system.name": "sqlite"}}}}}}
}
func hasFailure(r Report, code string) bool {
	for _, f := range r.Failures {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestAncestryAndGraph(t *testing.T) {
	if r := Check(fixture(), baseSpec()); !r.OK() {
		t.Fatalf("valid browser→API→DB: %+v", r)
	}
	for _, tc := range []struct {
		name, code string
		change     func([]Span) []Span
	}{
		{"detached", "broken_ancestry", func(s []Span) []Span { s[2].ParentSpanID = ""; return s }},
		{"missing-parent", "unresolved_parent", func(s []Span) []Span { s[2].ParentSpanID = id(9); return s }},
		{"cycle", "parent_cycle", func(s []Span) []Span { s[0].ParentSpanID = id(3); return s }},
		{"duplicate", "duplicate_span_id", func(s []Span) []Span { return append(s, s[2]) }},
		{"different-trace-same-id", "disconnected", func(s []Span) []Span { other := s[0]; other.TraceID = traceB; return append(s, other) }},
		{"invalid-id", "invalid_id", func(s []Span) []Span { s[0].TraceID = "invalid-secret"; return s }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Check(tc.change(fixture()), baseSpec())
			if !hasFailure(r, tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r)
			}
		})
	}
	// An additional bad terminal must fail even though the first path passes.
	s := fixture()
	bad := s[2]
	bad.SpanID = id(4)
	bad.ParentSpanID = id(1)
	s = append(s, bad)
	if !hasFailure(Check(s, baseSpec()), "broken_ancestry") {
		t.Fatal("passing sample concealed broken terminal")
	}
}

func TestDirectAndTemporalLinkAncestry(t *testing.T) {
	s := fixture()
	middle := Span{TraceID: traceA, SpanID: id(4), ParentSpanID: id(2), ServiceName: "middleware"}
	s[2].ParentSpanID = id(4)
	s = append(s, middle)
	spec := baseSpec()
	if !Check(s, spec).OK() {
		t.Fatal("transitive path failed")
	}
	spec.Ancestry[0].Direct = true
	if !hasFailure(Check(s, spec), "broken_ancestry") {
		t.Fatal("direct rule skipped intermediate")
	}
	s = []Span{{TraceID: traceA, SpanID: id(1), ServiceName: "api"}, {TraceID: traceB, SpanID: id(2), ServiceName: "temporal", Links: []Link{{TraceID: traceA, SpanID: id(1)}}}, {TraceID: traceB, SpanID: id(3), ParentSpanID: id(2), ServiceName: "worker"}}
	spec = Spec{Version: 1, Ancestry: []Ancestry{{Name: "workflow", Path: []Selector{{Service: "api"}, {Service: "temporal"}, {Service: "worker"}}, AllowLinks: true}}}
	if r := Check(s, spec); !r.OK() {
		t.Fatalf("cross-trace link: %+v", r)
	}
	spec.Ancestry[0].AllowLinks = false
	if !hasFailure(Check(s, spec), "broken_ancestry") {
		t.Fatal("parent-only traversal used link")
	}
	// A link cycle must terminate and cannot invent the absent API stage.
	s[1].Links = []Link{{TraceID: traceB, SpanID: id(3)}}
	spec.Ancestry[0].AllowLinks = true
	if Check(s, spec).OK() {
		t.Fatal("cyclic links passed")
	}
}

func TestAncestryMinimumCountsDistinctTerminalSpans(t *testing.T) {
	spans := fixture()
	spans = append(spans, spans[2])
	spec := baseSpec()
	spec.NoDuplicateSpanIDs = false
	spec.Ancestry[0].Min = ptr(2)
	if report := Check(spans, spec); !hasFailure(report, "missing_ancestry") {
		t.Fatal("a repeated capture inflated the distinct terminal-span minimum")
	}
}

func TestCountsSelectorsAndDuplicateOperations(t *testing.T) {
	s := fixture()
	spec := Spec{Version: 1, Counts: []Count{
		{Name: "root", Select: Selector{ParentSpanID: ptr("")}, Min: 1, Max: ptr(1)},
		{Name: "no-errors", Select: Selector{StatusCode: ptr(2)}, Max: ptr(0)},
		{Name: "database", Select: Selector{ServicePrefix: "ap", NamePrefix: "que", HasAttributes: []string{"db.query.text"}, AbsentAttributes: []string{"missing"}}, Min: 1, Max: ptr(1)},
	}, NoDuplicateOperations: []DuplicateOperations{{Name: "once", Select: Selector{Kind: ptr(3)}, GroupBy: []string{"trace_id", "parent_span_id", "name"}}}}
	if r := Check(s, spec); !r.OK() {
		t.Fatalf("selectors: %+v", r)
	}
	copy := s[2]
	copy.SpanID = id(4)
	s = append(s, copy)
	if r := Check(s, spec); !hasFailure(r, "duplicate_operation") || !hasFailure(r, "count_out_of_bounds") {
		t.Fatalf("duplicate: %+v", r)
	}
	spec.NoDuplicateOperations[0].GroupBy = []string{"attributes.missing"}
	if !hasFailure(Check(s, spec), "missing_group_field") {
		t.Fatal("missing tuple component ignored")
	}
}

func TestRedactionRecursiveAndSQLReasonScope(t *testing.T) {
	spec := Spec{Version: 1, Redaction: Redaction{ForbiddenKeys: []string{"password"}, ForbiddenValues: []string{"private-value"}, RequireQueryTextReason: true, ValidateDiagnostics: true}}
	if r := Check(fixture(), spec); !r.OK() {
		t.Fatalf("safe SQL: %+v", r)
	}
	for _, attrs := range []map[string]any{
		{"db.system.name": "redis", "db.operation.name": "GET"},
		{"db.system.name": "postgresql", "db.operation.name": "COMMIT", "db.query.diagnostics": []any{}},
		{"db.system.name": "postgresql", "db.operation.name": "ACQUIRE"},
		{"db.system.name": "postgresql"},
		{"db.system.name": "postgresql", "gotel.db.query.analysis_status": "malformed", "gotel.db.query.text_reason": "malformed", "db.query.diagnostics": []any{}},
	} {
		s := fixture()
		s[2].Attributes = attrs
		if r := Check(s, spec); !r.OK() {
			t.Fatalf("non-query/policy: %+v", r)
		}
	}
	s := fixture()
	s[2].Attributes = map[string]any{"db.system.name": "sqlite", "gotel.db.query.original_length": 13}
	if !hasFailure(Check(s, spec), "missing_or_invalid_text_reason") {
		t.Fatal("missing SQL omission reason passed")
	}
	s = fixture()
	s[2].Attributes["gotel.db.query.text_reason"] = "policy"
	s[2].Events = []any{map[string]any{"attributes": map[string]any{"nested": []any{map[string]any{"password": "prefix-private-value"}}}}}
	r := Check(s, spec)
	if !hasFailure(r, "forbidden_key") || !hasFailure(r, "forbidden_value") || !hasFailure(r, "unexpected_text_reason") {
		t.Fatalf("redaction failures: %+v", r)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "private-value") {
		t.Fatal("failure report leaked capture value")
	}
	s = fixture()
	s[2].Attributes["db.query.diagnostics"] = []any{map[string]any{"code": "invented", "message": "private-value"}}
	if !hasFailure(Check(s, spec), "invalid_diagnostic_wire_format") {
		t.Fatal("invalid diagnostics passed")
	}
}

func TestSpecStrictnessAndFindingAssociations(t *testing.T) {
	for _, value := range []string{`{}`, `{"version":2}`, `{"version":1}`, `{"version":1,"no_duplicate_span_ids":true,"typo":true}`, `{"version":1,"counts":[{"name":"bad","min":2,"max":1}]}`, `{"version":1,"no_duplicate_span_ids":true} {}`, `{"version":1,"ancestry":[{"name":"a","path":[{}]}]}`} {
		if _, err := DecodeSpec(strings.NewReader(value)); err == nil {
			t.Fatalf("accepted invalid spec %s", value)
		}
	}
	spec := Spec{Version: 1, Findings: []FindingRule{{Name: "query-name", RuleID: "db.storage_name_collision", SpanIDs: []string{id(1), id(2)}, Exact: true}}}
	findings, err := DecodeFindings(strings.NewReader(`{"data":[{"rule_id":"db.storage_name_collision","span_id":"0000000000000001"},{"rule_id":"db.storage_name_collision","span_id":"0000000000000002"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if r := CheckWithFindings(nil, findings, spec); !r.OK() {
		t.Fatalf("matching IDs: %+v", r)
	}
	findings[1].SpanID = id(3)
	r := CheckWithFindings(nil, findings, spec)
	if !hasFailure(r, "missing_finding") || !hasFailure(r, "unexpected_finding") {
		t.Fatalf("controls: %+v", r)
	}
}

func TestDecodeOTLPAndNormalizedRoundTrip(t *testing.T) {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "api")
	s := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	s.SetTraceID(pcommon.TraceID{15: 1})
	s.SetSpanID(pcommon.SpanID{7: 2})
	s.SetParentSpanID(pcommon.SpanID{7: 1})
	s.SetName("query")
	s.SetKind(ptrace.SpanKindClient)
	s.Status().SetCode(ptrace.StatusCodeError)
	s.Attributes().PutStr("db.query.text", "SELECT ?")
	e := s.Events().AppendEmpty()
	e.SetName("error")
	e.Attributes().PutStr("code", "safe")
	l := s.Links().AppendEmpty()
	l.SetTraceID(pcommon.TraceID{15: 2})
	l.SetSpanID(pcommon.SpanID{7: 3})
	for _, format := range []string{"json", "otlp-json", "otlp-proto"} {
		var b []byte
		var err error
		if format == "otlp-proto" {
			b, err = (&ptrace.ProtoMarshaler{}).MarshalTraces(traces)
		} else {
			b, err = (&ptrace.JSONMarshaler{}).MarshalTraces(traces)
		}
		if err != nil {
			t.Fatal(err)
		}
		rows, err := Decode(strings.NewReader(string(b)), format)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s: %v", format, err)
		}
		got := rows[0]
		if got.TraceID != traceA || got.SpanID != id(2) || got.ParentSpanID != id(1) || got.ServiceName != "api" || got.StatusCode != 2 || len(got.Events) != 1 || len(got.Links) != 1 {
			t.Fatalf("lost capture fields: %+v", got)
		}
	}
	for _, input := range []string{`[{"trace_id":"` + traceA + `","span_id":"` + id(2) + `","attributes":"{\"db.query.text\":\"SELECT ?\"}","resource":"{}"}]`, `{"data":[{"trace_id":"` + traceA + `","span_id":"` + id(2) + `","attributes":{"db.query.text":"SELECT ?"}}]}`} {
		rows, err := Decode(strings.NewReader(input), "json")
		if err != nil || rows[0].Attributes["db.query.text"] != "SELECT ?" {
			t.Fatalf("normalized %v", err)
		}
	}
	for _, input := range []string{`{}`, `null`, `[{"attributes":"malformed-private-value"}]`, `{"resourceSpans":"private-value"}`} {
		_, err := Decode(strings.NewReader(input), "json")
		if err == nil {
			t.Fatal("accepted malformed input")
		}
		if strings.Contains(err.Error(), "private-value") {
			t.Fatal("decode leaked capture")
		}
	}
}

func TestFetchTraceLoopback(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces/"+traceA {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": fixture()})
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()
	rows, err := FetchTrace(context.Background(), server.Client(), server.URL, traceA)
	if err != nil || len(rows) != 3 {
		t.Fatalf("fetch %v", err)
	}
	if _, err = FetchTrace(context.Background(), server.Client(), server.URL, "../../secret"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err = FetchTrace(context.Background(), server.Client(), server.URL, traceB); err == nil {
		t.Fatal("404 accepted")
	}
}
