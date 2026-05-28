package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/storage"
)

type diffStub struct {
	server         *httptest.Server
	recordedQuery  string
	expectBaseline string
	expectCompare  string
}

func newDiffStub(t *testing.T, sessions []storage.Session, result api.DiffResult) *diffStub {
	t.Helper()
	s := &diffStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": sessions})
	})
	mux.HandleFunc("/api/diff", func(w http.ResponseWriter, r *http.Request) {
		s.recordedQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func sampleDiff() api.DiffResult {
	return api.DiffResult{
		Baseline: api.DiffSessionInfo{
			SessionID: "88d15f24b1234567890abcde", Label: "main",
			TotalDurationNs: 612_000_000, SpanCount: 24, DBCalls: 9,
		},
		Compare: api.DiffSessionInfo{
			SessionID: "9ef3d19eb1234567890fedcba", Label: "feat/checkout",
			TotalDurationNs: 472_000_000, SpanCount: 20, DBCalls: 5,
		},
		Summary: api.DiffSummary{
			DurationDeltaNs:  -140_000_000,
			DurationDeltaPct: -23.0,
			SpansAdded:       0,
			SpansRemoved:     0,
			DBCallDelta:      -4,
		},
		Spans: []api.DiffSpan{
			{Name: "payment.Charge", ServiceName: "payment-service", Status: "changed",
				BaselineDurationNs: 308_000_000, CompareDurationNs: 340_000_000, DeltaPct: 10.4},
			{Name: "POST /api/checkout/confirm", ServiceName: "api", Status: "added",
				CompareDurationNs: 40_000_000},
			{Name: "pricing.Quote.child1", ServiceName: "pricing-service", Status: "removed",
				BaselineDurationNs: 20_000_000},
		},
	}
}

func TestRunDiff_JSONFormat(t *testing.T) {
	stub := newDiffStub(t, []storage.Session{
		{ID: "id-base", Label: "base"},
		{ID: "id-cmp", Label: "cmp"},
	}, sampleDiff())

	var buf bytes.Buffer
	if err := runDiff(stub.server.URL, "id-base", "id-cmp", 100, true, &buf); err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	var parsed api.DiffResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("--json output is not parseable: %v\n%s", err, buf.String())
	}
	if parsed.Summary.DurationDeltaPct != -23.0 {
		t.Errorf("round-tripped summary lost data: %+v", parsed.Summary)
	}
}

func TestRunDiff_JSONResolvesLabelsToIDs(t *testing.T) {
	stub := newDiffStub(t, []storage.Session{
		{ID: "88d15f24b1234567890abcde", Label: "main"},
		{ID: "9ef3d19eb1234567890fedcba", Label: "feat/checkout"},
	}, sampleDiff())

	var buf bytes.Buffer
	if err := runDiff(stub.server.URL, "main", "feat/checkout", 100, true, &buf); err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	if !strings.Contains(stub.recordedQuery, "baseline=88d15f24b1234567890abcde") ||
		!strings.Contains(stub.recordedQuery, "compare=9ef3d19eb1234567890fedcba") {
		t.Errorf("diff query did not carry resolved ids: %q", stub.recordedQuery)
	}
}

func TestDiffFailsGate_Threshold(t *testing.T) {
	r := sampleDiff()
	r.Summary.DurationDeltaPct = 12.0
	if !diffFailsGate(r, 5) {
		t.Error("expected gate to trip when delta exceeds threshold")
	}
	if diffFailsGate(r, 50) {
		t.Error("expected gate to pass when threshold is higher than delta")
	}
}

func TestDiffFailsGate_SpansAdded(t *testing.T) {
	r := sampleDiff()
	r.Summary.DurationDeltaPct = 0
	r.Summary.SpansAdded = 1
	if !diffFailsGate(r, 100) {
		t.Error("any spans_added > 0 should trip the gate")
	}
	r.Summary.SpansAdded = 0
	if diffFailsGate(r, 100) {
		t.Error("clean diff should not trip the gate")
	}
}

func TestRunDiff_JSONGateTrips(t *testing.T) {
	d := sampleDiff()
	d.Summary.DurationDeltaPct = 12.0
	stub := newDiffStub(t, []storage.Session{
		{ID: "a", Label: "a"}, {ID: "b", Label: "b"},
	}, d)
	err := runDiff(stub.server.URL, "a", "b", 5, true, &bytes.Buffer{})
	if err != errDiffRegressed {
		t.Errorf("threshold=5 vs delta=12: want errDiffRegressed, got %v", err)
	}
}

func TestResolveSessionRef_UnknownRef(t *testing.T) {
	stub := newDiffStub(t, []storage.Session{
		{ID: "x", Label: "x"},
	}, api.DiffResult{})

	_, err := resolveSessionRef(stub.server.URL, "missing")
	if err == nil || !strings.Contains(err.Error(), "no session") {
		t.Errorf("expected 'no session' error, got %v", err)
	}
}

func TestResolveSessionRef_IdMatchWinsOverLabel(t *testing.T) {
	stub := newDiffStub(t, []storage.Session{
		{ID: "real-id", Label: "real-label"},
		{ID: "decoy", Label: "real-id"},
	}, api.DiffResult{})

	got, err := resolveSessionRef(stub.server.URL, "real-id")
	if err != nil {
		t.Fatalf("resolveSessionRef: %v", err)
	}
	if got != "real-id" {
		t.Errorf("id match should beat label match, got %q", got)
	}
}

func TestRunDiff_MissingFlags(t *testing.T) {
	err := runDiff("http://unused", "", "anything", 10, true, &bytes.Buffer{})
	if err == nil {
		t.Errorf("expected error for empty baseline ref")
	}
}

func TestDiffModel_ViewShowsRegressedAndAdded(t *testing.T) {
	m := newDiffModel(sampleDiff())
	out := m.View()
	for _, want := range []string{
		"main", "feat/checkout",
		"payment-service", "payment.Charge",
		"POST /api/checkout/confirm",
		"pricing.Quote.child1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff view missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestFmtNs(t *testing.T) {
	cases := map[int64]string{
		0:             "0ns",
		999:           "999ns",
		1_000:         "1.0µs",
		1_500_000:     "1.5ms",
		612_000_000:   "612.0ms",
		1_840_000_000: "1.84s",
	}
	for ns, want := range cases {
		if got := fmtNs(ns); got != want {
			t.Errorf("fmtNs(%d): want %q, got %q", ns, want, got)
		}
	}
}
