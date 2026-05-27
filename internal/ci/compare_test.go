package ci

import (
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

func mkSpan(svc, name string, parent string, dur int64, status int, traceID string) *storage.Span {
	return &storage.Span{
		TraceID:      traceID,
		SpanID:       svc + "-" + name + "-" + traceID,
		ParentSpanID: parent,
		ServiceName:  svc,
		Name:         name,
		StartNs:      0,
		EndNs:        dur,
		DurationNs:   dur,
		StatusCode:   status,
	}
}

func TestBuildSnapshot_BasicAggregation(t *testing.T) {
	spans := []*storage.Span{
		mkSpan("api", "GET /cart", "", 100, 0, "t1"), // root
		mkSpan("db",  "SELECT",    "api-GET /cart-t1", 30, 0, "t1"),
		mkSpan("db",  "SELECT",    "api-GET /cart-t1", 50, 0, "t1"),
		mkSpan("db",  "SELECT",    "api-GET /cart-t1", 40, 0, "t1"),
	}
	snap := BuildSnapshot("sess", 12345, spans, nil)

	if snap.Version != SnapshotVersion {
		t.Errorf("version = %d, want %d", snap.Version, SnapshotVersion)
	}
	if snap.TraceCount != 1 || snap.SpanCount != 4 {
		t.Errorf("counts wrong: traces=%d spans=%d", snap.TraceCount, snap.SpanCount)
	}
	if snap.RootDurationNs != 100 {
		t.Errorf("RootDurationNs = %d, want 100", snap.RootDurationNs)
	}
	// 3 db spans, p95 idx = 3*95/100 = 2 → sorted[2] = 50
	var dbOp *Operation
	for i := range snap.Operations {
		if snap.Operations[i].Service == "db" {
			dbOp = &snap.Operations[i]
		}
	}
	if dbOp == nil {
		t.Fatal("missing db op")
	}
	if dbOp.Count != 3 || dbOp.P95Ns != 50 {
		t.Errorf("db op: count=%d p95=%d, want count=3 p95=50", dbOp.Count, dbOp.P95Ns)
	}
}

func TestBuildSnapshot_ErrorsAndN1(t *testing.T) {
	spans := []*storage.Span{
		mkSpan("api", "GET /broken", "", 50, 2, "t1"), // error
		mkSpan("api", "GET /broken", "", 60, 2, "t2"), // error
		mkSpan("api", "GET /ok",     "", 10, 0, "t3"),
	}
	issues := []*storage.TraceIssue{
		{Kind: "n_plus_one", Fingerprint: "SELECT * FROM users WHERE id = ?", Count: 12, WastedNs: 2000},
		{Kind: "something_else", Fingerprint: "ignored", Count: 99},
	}
	snap := BuildSnapshot("sess", 0, spans, issues)

	if len(snap.NPlusOne) != 1 || snap.NPlusOne[0].Count != 12 {
		t.Errorf("expected one N+1 with count 12, got %+v", snap.NPlusOne)
	}
	var errOp *ErrorOp
	for i := range snap.Errors {
		if snap.Errors[i].Name == "GET /broken" {
			errOp = &snap.Errors[i]
		}
	}
	if errOp == nil || errOp.Count != 2 {
		t.Errorf("expected GET /broken error op with count 2, got %+v", snap.Errors)
	}
}

func snap(ops []Operation, n1 []N1Group, errs []ErrorOp, root int64) Snapshot {
	return Snapshot{
		Version:        SnapshotVersion,
		Operations:     ops,
		NPlusOne:       n1,
		Errors:         errs,
		RootDurationNs: root,
	}
}

func TestCompare_PassWhenWithinThreshold(t *testing.T) {
	base := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 100, Count: 5}}, nil, nil, 200)
	cur := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 115, Count: 5}}, nil, nil, 230)
	res := Compare(base, cur, DefaultCheckOptions())
	if !res.Pass {
		t.Errorf("expected pass within 20%% threshold, got regressions=%+v", res.Regressions)
	}
}

func TestCompare_DurationRegressionFails(t *testing.T) {
	base := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 100}}, nil, nil, 0)
	cur := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 150}}, nil, nil, 0)
	res := Compare(base, cur, DefaultCheckOptions())
	if res.Pass {
		t.Fatalf("expected failure, got pass")
	}
	if len(res.Regressions) != 1 || res.Regressions[0].Kind != "duration" {
		t.Errorf("expected single duration regression, got %+v", res.Regressions)
	}
}

func TestCompare_RootDurationRegression(t *testing.T) {
	base := snap(nil, nil, nil, 100)
	cur := snap(nil, nil, nil, 150)
	res := Compare(base, cur, DefaultCheckOptions())
	if res.Pass || len(res.Regressions) != 1 || res.Regressions[0].Kind != "root_duration" {
		t.Errorf("expected root_duration regression, got %+v", res)
	}
}

func TestCompare_NewN1Fails(t *testing.T) {
	cur := snap(nil, []N1Group{{Fingerprint: "SELECT * FROM users WHERE id = ?", Count: 12}}, nil, 0)
	res := Compare(Snapshot{}, cur, DefaultCheckOptions())
	if res.Pass || res.Regressions[0].Kind != "n_plus_one" {
		t.Errorf("expected n_plus_one regression for new fingerprint, got %+v", res)
	}
}

func TestCompare_ExistingN1NotRegressionUnlessExceedsThreshold(t *testing.T) {
	base := snap(nil, []N1Group{{Fingerprint: "fp", Count: 12}}, nil, 0)
	cur := snap(nil, []N1Group{{Fingerprint: "fp", Count: 15}}, nil, 0) // +3, below default 10
	if !Compare(base, cur, DefaultCheckOptions()).Pass {
		t.Errorf("expected pass when N+1 growth below threshold")
	}
	cur2 := snap(nil, []N1Group{{Fingerprint: "fp", Count: 25}}, nil, 0) // +13, above 10
	res := Compare(base, cur2, DefaultCheckOptions())
	if res.Pass || res.Regressions[0].Kind != "n_plus_one" {
		t.Errorf("expected n_plus_one regression for growth past threshold, got %+v", res)
	}
}

func TestCompare_NewErrorFails(t *testing.T) {
	cur := snap(nil, nil, []ErrorOp{{Service: "api", Name: "GET /x", Count: 1}}, 0)
	res := Compare(Snapshot{}, cur, DefaultCheckOptions())
	if res.Pass || res.Regressions[0].Kind != "error" {
		t.Errorf("expected error regression, got %+v", res)
	}
}

func TestCompare_KnownErrorStillFailing_NotARegression(t *testing.T) {
	base := snap(nil, nil, []ErrorOp{{Service: "api", Name: "GET /x", Count: 1}}, 0)
	cur := snap(nil, nil, []ErrorOp{{Service: "api", Name: "GET /x", Count: 3}}, 0)
	if !Compare(base, cur, DefaultCheckOptions()).Pass {
		t.Errorf("expected pass when error existed in baseline")
	}
}

func TestCompare_CustomThreshold(t *testing.T) {
	base := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 100}}, nil, nil, 0)
	cur := snap([]Operation{{Service: "api", Name: "GET /", P95Ns: 105}}, nil, nil, 0) // 5% rise
	// Strict 1% threshold flips this to a regression.
	res := Compare(base, cur, CheckOptions{DurationThresholdPct: 1})
	if res.Pass {
		t.Errorf("expected regression with 1%% threshold, got pass")
	}
}
