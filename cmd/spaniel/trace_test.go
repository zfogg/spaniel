package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cannedTrace() []*traceSpan {
	const traceID = "0123456789abcdef0123456789abcdef"
	return []*traceSpan{
		{TraceID: traceID, SpanID: "aaaa", ParentSpanID: "", ServiceName: "checkout-api", Name: "GET /cart",
			StartNs: 1_000_000_000, EndNs: 1_500_000_000, DurationNs: 500_000_000, StatusCode: 1},
		{TraceID: traceID, SpanID: "bbbb", ParentSpanID: "aaaa", ServiceName: "cart-svc", Name: "rpc.LoadCart",
			StartNs: 1_100_000_000, EndNs: 1_300_000_000, DurationNs: 200_000_000, StatusCode: 1},
		{TraceID: traceID, SpanID: "cccc", ParentSpanID: "bbbb", ServiceName: "db", Name: "SELECT users",
			StartNs: 1_150_000_000, EndNs: 1_250_000_000, DurationNs: 100_000_000, StatusCode: 2, Tag: "error"},
	}
}

func newTraceServer(t *testing.T, spans []*traceSpan) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/traces/") && len(r.URL.Path) > len("/api/traces/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/traces/")
			if id != spans[0].TraceID {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": spans}) //nolint:errcheck
		case r.URL.Path == "/api/traces":
			type listEntry struct {
				TraceID string `json:"trace_id"`
			}
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"data": []listEntry{{TraceID: spans[0].TraceID}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchTrace_ExactID(t *testing.T) {
	spans := cannedTrace()
	srv := newTraceServer(t, spans)
	defer srv.Close()

	got, err := fetchTrace(srv.URL, spans[0].TraceID)
	if err != nil {
		t.Fatalf("fetchTrace: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 spans, got %d", len(got))
	}
}

func TestFetchTrace_ShortIDPrefix(t *testing.T) {
	spans := cannedTrace()
	srv := newTraceServer(t, spans)
	defer srv.Close()

	got, err := fetchTrace(srv.URL, spans[0].TraceID[:4])
	if err != nil {
		t.Fatalf("fetchTrace short-id: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 spans via short-id lookup, got %d", len(got))
	}
}

func TestFetchTrace_NotFound(t *testing.T) {
	spans := cannedTrace()
	srv := newTraceServer(t, spans)
	defer srv.Close()

	_, err := fetchTrace(srv.URL, "zzzz")
	if err == nil {
		t.Fatal("expected error for unknown trace id, got nil")
	}
}

func TestComputeDepths_OrphanParentIsRoot(t *testing.T) {
	spans := []*traceSpan{
		{SpanID: "a", ParentSpanID: ""},
		{SpanID: "b", ParentSpanID: "ghost"},
		{SpanID: "c", ParentSpanID: "a"},
		{SpanID: "d", ParentSpanID: "c"},
	}
	d := computeDepths(spans)
	if d["a"] != 0 || d["b"] != 0 {
		t.Errorf("roots: a=%d b=%d, want 0 0", d["a"], d["b"])
	}
	if d["c"] != 1 {
		t.Errorf("c depth = %d, want 1", d["c"])
	}
	if d["d"] != 2 {
		t.Errorf("d depth = %d, want 2", d["d"])
	}
}

func TestTraceWindow_FindsMinStartMaxEnd(t *testing.T) {
	spans := cannedTrace()
	start, end := traceWindow(spans)
	if start != 1_000_000_000 {
		t.Errorf("start = %d, want 1_000_000_000", start)
	}
	if end != 1_500_000_000 {
		t.Errorf("end = %d, want 1_500_000_000", end)
	}
}

func TestSortSpansByStart_StableOrder(t *testing.T) {
	spans := cannedTrace()
	sorted := sortSpansByStart(spans)
	for i := 1; i < len(sorted); i++ {
		if sorted[i].StartNs < sorted[i-1].StartNs {
			t.Errorf("not sorted: idx %d StartNs=%d < idx %d StartNs=%d",
				i, sorted[i].StartNs, i-1, sorted[i-1].StartNs)
		}
	}
}

func TestHumanNs(t *testing.T) {
	cases := map[int64]string{
		500:           "500ns",
		1_500:         "1µs",
		1_500_000:     "1ms",
		1_500_000_000: "1.50s",
	}
	for in, want := range cases {
		if got := humanNs(in); got != want {
			t.Errorf("humanNs(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTagLabel(t *testing.T) {
	cases := map[string]string{
		"n_plus_one": "[N+1]",
		"error":      "[ERROR]",
		"slow":       "[SLOW]",
		"lint":       "[LINT]",
		"unknown":    "[UNKNOWN]",
	}
	for in, want := range cases {
		if got := tagLabel(in); got != want {
			t.Errorf("tagLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTraceModel_ViewContainsServicesAndIDs(t *testing.T) {
	m := newTraceModel(cannedTrace())
	// Simulate a window-size message so View renders something meaningful.
	out := m.View()
	for _, want := range []string{"checkout-api", "GET /cart", "cart-svc", "db", "SELECT users"} {
		if !strings.Contains(out, want) {
			t.Errorf("trace view missing %q\n--- got ---\n%s", want, out)
		}
	}
}
