package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/storage"
)

// seedSubcommand returns `spaniel seed` — writes a deterministic fixture
// set so the UI has interesting data to look at without running a real app.
//
// Idempotency: all rows are scoped to two fixed session IDs (recent +
// baseline). Each run deletes everything in those sessions first, then
// re-inserts. Running twice leaves the DB in exactly the same state.
func seedSubcommand(v interface{ GetString(string) string }) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Upsert a deterministic fixture set into the DB (idempotent)",
		Long: `Writes ~200 spans / ~60 logs / ~180 metric data-points across
2 sessions (an active "seed-recent" and a "seed-baseline") covering 3
services. Includes a deliberate N+1 trace, errors, slow spans, and a
cross-trace span link so every view has something to render.

Re-running deletes the seed-scoped rows and reinserts — your real data
is untouched.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dbPath == "" {
				dbPath = v.GetString("db_path")
			}
			if dbPath == "" {
				dbPath = defaultDBPath()
			}
			return runSeed(cmd.OutOrStdout(), expandHome(dbPath))
		},
	}
	cmd.Flags().StringVar(&dbPath, "db-path", "", "Override DB path (defaults to config db_path)")
	cmd.SilenceUsage = true
	return cmd
}

const (
	seedSessionRecentID   = "seed-recent-session-0000-0000-0000"
	seedSessionRecentLbl  = "seed-recent"
	seedSessionBaseID     = "seed-baseline-session-0000-0000-0000"
	seedSessionBaseLbl    = "seed-baseline"
)

var seedServices = []string{"checkout-api", "cart-svc", "payments-svc"}

func runSeed(out interface{ Write([]byte) (int, error) }, dbPath string) error {
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close() //nolint:errcheck

	if err := clearSeedSessions(store); err != nil {
		return fmt.Errorf("clear prior seed rows: %w", err)
	}
	if err := upsertSeedSessions(store); err != nil {
		return fmt.Errorf("upsert sessions: %w", err)
	}

	stats := seedStats{}
	// Active session: recent traffic with a real-time window so the trace
	// list/log tail/metrics charts all show something within "last hour".
	if err := seedSession(store, seedSessionRecentID, seedSessionRecentLbl, time.Now().Add(-45*time.Minute), &stats); err != nil {
		return fmt.Errorf("seed recent session: %w", err)
	}
	// Baseline: older window, mostly clean — diff/comparison views need a
	// "before" snapshot.
	if err := seedSession(store, seedSessionBaseID, seedSessionBaseLbl, time.Now().Add(-2*time.Hour), &stats); err != nil {
		return fmt.Errorf("seed baseline session: %w", err)
	}

	// Activate the recent session so the UI lands on populated data.
	store.SetActiveSession(seedSessionRecentID, seedSessionRecentLbl)

	fmt.Fprintf(out, "seeded ✓\n")
	fmt.Fprintf(out, "  sessions  %d  (%s active, %s baseline)\n", 2, seedSessionRecentLbl, seedSessionBaseLbl)
	fmt.Fprintf(out, "  services  %d  (%v)\n", len(seedServices), seedServices)
	fmt.Fprintf(out, "  traces    %d\n", stats.traces)
	fmt.Fprintf(out, "  spans     %d  (%d errors, %d slow, 1 N+1 per session)\n", stats.spans, stats.errors, stats.slow)
	fmt.Fprintf(out, "  logs      %d\n", stats.logs)
	fmt.Fprintf(out, "  metrics   %d data points\n", stats.metrics)
	fmt.Fprintf(out, "  links     %d cross-trace span links\n", stats.links)
	fmt.Fprintf(out, "\nopen the UI at http://localhost:%d to view (or whatever port is in your config)\n", 8080)
	return nil
}

type seedStats struct {
	traces, spans, errors, slow, logs, metrics, links int
}

// clearSeedSessions removes every row tagged with a seed session id from
// every table that has a session_id column. Idempotency depends on this
// being exhaustive — add new tables here when they're added to storage.
func clearSeedSessions(store *storage.DB) error {
	ids := []string{seedSessionRecentID, seedSessionBaseID}
	db := store.SQL()
	for _, id := range ids {
		for _, table := range []string{
			"spans", "logs", "metrics", "span_events", "span_links",
			"lint_warnings", "trace_issues",
		} {
			if _, err := db.Exec("DELETE FROM "+table+" WHERE session_id = ?", id); err != nil {
				return fmt.Errorf("DELETE FROM %s: %w", table, err)
			}
		}
		if _, err := db.Exec("DELETE FROM sessions WHERE id = ?", id); err != nil {
			return fmt.Errorf("DELETE FROM sessions: %w", err)
		}
	}
	return nil
}

// upsertSeedSessions inserts the two fixed sessions with their fixed IDs.
// The PK on sessions makes this a real upsert with INSERT OR REPLACE.
func upsertSeedSessions(store *storage.DB) error {
	now := time.Now().UnixNano()
	servicesJSON, _ := json.Marshal(seedServices)
	rows := []struct {
		id, label string
		baseline  bool
		createdAt int64
	}{
		{seedSessionRecentID, seedSessionRecentLbl, false, now},
		{seedSessionBaseID, seedSessionBaseLbl, true, now - int64(2*time.Hour)},
	}
	for _, r := range rows {
		if _, err := store.SQL().Exec(`
			INSERT OR REPLACE INTO sessions
			  (id, label, created_at, is_baseline, is_imported, span_count, services)
			VALUES (?, ?, ?, ?, false, 0, ?)`,
			r.id, r.label, r.createdAt, r.baseline, string(servicesJSON),
		); err != nil {
			return err
		}
	}
	return nil
}

// seedSession populates one session's worth of traces, logs, and metrics.
// All randomness is seeded deterministically from the session id so re-runs
// produce identical data.
func seedSession(store *storage.DB, sessionID, sessionLabel string, windowStart time.Time, stats *seedStats) error {
	rng := rand.New(rand.NewPCG(seedFromString(sessionID), 0x5eed))
	startNs := windowStart.UnixNano()

	const traceCount = 14
	traceIDs := make([]string, 0, traceCount)

	for t := 0; t < traceCount; t++ {
		traceID := deterministicID(sessionID, "trace", t, 32)
		traceIDs = append(traceIDs, traceID)
		// Stagger trace starts across the window so the trace list isn't all clumped.
		traceStart := startNs + int64(t)*int64(2*time.Minute)
		spans := buildTraceSpans(rng, traceID, sessionID, sessionLabel, traceStart)
		for _, sp := range spans {
			if err := store.InsertSpan(sp); err != nil {
				return err
			}
			stats.spans++
			if sp.StatusCode >= 2 {
				stats.errors++
			}
			// duration_ns is a DB-generated column, not set on the struct —
			// compute it here to match spaniel's 250ms slow threshold.
			if sp.EndNs-sp.StartNs > 250*int64(time.Millisecond) {
				stats.slow++
			}
		}
		// Correlated logs: attach a couple to the root span and one to the DB
		// span so the inspector's per-span "logs" tab is populated when you
		// click around in /traces/<id>. span_id + trace_id are set so
		// /api/logs?spanId=… returns them.
		root, dbSpan := spans[0], spans[len(spans)-1]
		logs := logsForSpan(rng, sessionID, root, 2)
		logs = append(logs, logsForSpan(rng, sessionID, dbSpan, 1)...)
		if root.StatusCode >= 2 {
			logs = append(logs, errorLogForSpan(sessionID, root))
		}
		for _, l := range logs {
			if err := store.InsertLog(l); err != nil {
				return err
			}
			stats.logs++
		}
		stats.traces++
	}

	// One deliberate N+1 trace per session.
	n1TraceID := deterministicID(sessionID, "n1", 0, 32)
	n1Spans := buildN1TraceSpans(n1TraceID, sessionID, sessionLabel, startNs+int64(traceCount*2)*int64(time.Minute))
	for _, sp := range n1Spans {
		if err := store.InsertSpan(sp); err != nil {
			return err
		}
		stats.spans++
	}
	// A warning log on the N+1 root so the trace's logs tab tells the story.
	n1Log := logsForSpan(rng, sessionID, n1Spans[0], 1)[0]
	n1Log.Severity = 13
	n1Log.Body = "slow endpoint: 12 sequential queries to users table (likely N+1)"
	if err := store.InsertLog(n1Log); err != nil {
		return err
	}
	stats.logs++
	stats.traces++
	if err := store.UpsertTraceIssue(&storage.TraceIssue{
		ID:           deterministicID(sessionID, "issue", 0, 16),
		TraceID:      n1TraceID,
		SessionID:    sessionID,
		Kind:         "n_plus_one",
		Fingerprint:  "SELECT users WHERE id = ?",
		Count:        12,
		WastedNs:     250 * int64(time.Millisecond),
		ParentSpanID: n1Spans[0].SpanID,
		ExampleSpanID: n1Spans[1].SpanID,
		CreatedAt:    time.Now().UnixNano(),
	}); err != nil {
		return err
	}

	// Seed one trace per new detector kind so every view has something to render.
	detectorTraces := []struct {
		kind  string
		build func(traceID, sessID, sessLbl string, startNs int64) []*storage.Span
		issue func(traceID string, spans []*storage.Span) *storage.TraceIssue
	}{
		{
			kind:  "slow_db",
			build: buildSlowDBTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "slow_db", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "slow_db", Fingerprint: "SELECT reports", Count: 1,
					WastedNs: 130 * int64(time.Millisecond),
					ParentSpanID: spans[0].SpanID, ExampleSpanID: spans[1].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "chatty_http",
			build: buildChattyHTTPTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "chatty_http", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "chatty_http", Fingerprint: "pricing.internal", Count: 6,
					WastedNs: 180 * int64(time.Millisecond),
					ParentSpanID: spans[0].SpanID, ExampleSpanID: spans[1].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "tracing_gap",
			build: buildTracingGapTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "tracing_gap", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "tracing_gap", Fingerprint: "POST /api/charge", Count: 1,
					WastedNs: 350 * int64(time.Millisecond),
					ParentSpanID: "", ExampleSpanID: spans[0].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "error_chain",
			build: buildErrorChainTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "error_chain", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "error_chain", Fingerprint: "charge card", Count: 1,
					WastedNs: 0,
					ParentSpanID: spans[0].SpanID, ExampleSpanID: spans[1].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "serial_promise",
			build: buildSerialPromiseTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "serial_promise", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "serial_promise", Fingerprint: "GET /api/dashboard (4 serial calls)", Count: 4,
					WastedNs: 300 * int64(time.Millisecond),
					ParentSpanID: "", ExampleSpanID: spans[0].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "synchronous_io",
			build: buildSynchronousIOTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "synchronous_io", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "synchronous_io", Fingerprint: "GET /api/cart/summary (4 db calls)", Count: 4,
					WastedNs: 200 * int64(time.Millisecond),
					ParentSpanID: "", ExampleSpanID: spans[0].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "large_payload",
			build: buildLargePayloadTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "large_payload", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "large_payload", Fingerprint: "3145728 bytes", Count: 1,
					WastedNs: 3_145_728,
					ParentSpanID: spans[0].SpanID, ExampleSpanID: spans[1].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
		{
			kind:  "cache_miss_storm",
			build: buildCacheMissStormTrace,
			issue: func(tid string, spans []*storage.Span) *storage.TraceIssue {
				return &storage.TraceIssue{
					ID: deterministicID(sessionID, "cache_miss_storm", 0, 16), TraceID: tid, SessionID: sessionID,
					Kind: "cache_miss_storm", Fingerprint: "cache miss storm", Count: 12,
					WastedNs: 12 * int64(2*time.Millisecond),
					ParentSpanID: spans[0].SpanID, ExampleSpanID: spans[1].SpanID,
					CreatedAt: time.Now().UnixNano(),
				}
			},
		},
	}
	detectorStart := startNs + int64(traceCount+2)*int64(2*time.Minute)
	for i, dt := range detectorTraces {
		tid := deterministicID(sessionID+"-"+dt.kind, "trace", 0, 32)
		tStart := detectorStart + int64(i)*int64(3*time.Minute)
		spans := dt.build(tid, sessionID, sessionLabel, tStart)
		for _, sp := range spans {
			if err := store.InsertSpan(sp); err != nil {
				return err
			}
			stats.spans++
		}
		if err := store.UpsertTraceIssue(dt.issue(tid, spans)); err != nil {
			return err
		}
		stats.traces++
	}

	// A cross-trace link: trace[1] follows-from trace[0].
	if len(traceIDs) >= 2 {
		if err := store.InsertSpanLinks([]*storage.SpanLink{{
			SpanID:        traceIDs[1][:16], // root span id of trace[1]
			TraceID:       traceIDs[1],
			SessionID:     sessionID,
			LinkedTraceID: traceIDs[0],
			LinkedSpanID:  traceIDs[0][:16], // root span id of trace[0]
			TraceState:    "",
			Attributes:    `{"link.kind":"follows_from"}`,
		}}); err != nil {
			return err
		}
		stats.links++
	}

	// A handful of free-floating operational logs (no trace) so the global
	// /logs view has non-request noise too.
	for i := 0; i < 10; i++ {
		l := freeFloatingLog(rng, sessionID, startNs, i)
		if err := store.InsertLog(l); err != nil {
			return err
		}
		stats.logs++
	}

	// Metrics: per service × type × points-in-window
	if n, err := seedMetrics(store, sessionID, startNs); err != nil {
		return err
	} else {
		stats.metrics += n
	}

	return nil
}

// buildTraceSpans produces a small parent→child→grandchild span tree for one
// trace, with a sprinkle of attribute richness so the inspector has content.
func buildTraceSpans(rng *rand.Rand, traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootSvc := seedServices[rng.IntN(len(seedServices))]
	rootSpanID := traceID[:16]
	childSpanID := flipHex(traceID[:16])
	dbSpanID := flipHex(childSpanID)

	rootDur := time.Duration(80+rng.IntN(150)) * time.Millisecond
	childDur := rootDur - time.Duration(20+rng.IntN(40))*time.Millisecond
	dbDur := time.Duration(10+rng.IntN(120)) * time.Millisecond

	// ~35% of traces are deliberately slow: push the root well past spaniel's
	// 250ms slow threshold (db.go) so the 'slow' tag actually fires in the UI.
	slow := rng.Float64() < 0.35
	if slow {
		rootDur = time.Duration(300+rng.IntN(700)) * time.Millisecond
		childDur = rootDur - time.Duration(40+rng.IntN(60))*time.Millisecond
		// A slow DB call is the usual culprit — make it the long pole.
		dbDur = childDur - time.Duration(20+rng.IntN(40))*time.Millisecond
	}

	statusCode := 1
	statusMsg := ""
	// ~15% of traces are errors.
	if rng.Float64() < 0.15 {
		statusCode = 2
		statusMsg = "upstream returned 500"
	}

	method := []string{"GET", "POST", "PUT", "DELETE"}[rng.IntN(4)]
	route := []string{"/cart", "/checkout", "/api/users/:id", "/api/orders", "/api/products/:sku"}[rng.IntN(5)]
	httpStatus := 200
	if statusCode >= 2 {
		httpStatus = []int{500, 502, 503}[rng.IntN(3)]
	} else {
		httpStatus = []int{200, 200, 200, 201, 204}[rng.IntN(5)]
	}
	userID := fmt.Sprintf("u_%d", 1000+rng.IntN(9000))

	rootAttrs, _ := json.Marshal(map[string]any{
		"http.request.method":      method,
		"http.route":               route,
		"http.response.status_code": httpStatus,
		"url.path":                 route,
		"url.scheme":               "https",
		"server.address":           rootSvc + ".internal",
		"client.address":           fmt.Sprintf("10.0.%d.%d", rng.IntN(255), rng.IntN(255)),
		"user_agent.original":      "Mozilla/5.0 (seed-loadgen)",
		"enduser.id":               userID,
	})
	rootResource, _ := json.Marshal(map[string]any{
		"service.name":           rootSvc,
		"service.version":        "1.4.2",
		"deployment.environment": "seed",
		"host.name":              "pod-" + rootSvc + "-7f9c",
		"telemetry.sdk.language": "go",
	})
	childAttrs, _ := json.Marshal(map[string]any{
		"rpc.system":      "grpc",
		"rpc.service":     "cart.v1.CartService",
		"rpc.method":      "Load",
		"rpc.grpc.status_code": 0,
		"net.peer.name":   "cart-svc.internal",
		"net.peer.port":   8443,
		"enduser.id":      userID,
	})
	dbAttrs, _ := json.Marshal(map[string]any{
		"db.system":         "postgresql",
		"db.name":           "shop",
		"db.user":           "app",
		"db.statement":      "SELECT * FROM cart WHERE user_id = $1",
		"db.operation":      "SELECT",
		"db.sql.table":      "cart",
		"server.address":    "pg-primary.internal",
		"server.port":       5432,
	})
	childResource, _ := json.Marshal(map[string]any{
		"service.name":           "cart-svc",
		"service.version":        "0.9.1",
		"deployment.environment": "seed",
	})
	dbResource, _ := json.Marshal(map[string]any{
		"service.name":           "postgres",
		"deployment.environment": "seed",
	})

	root := &storage.Span{
		TraceID: traceID, SpanID: rootSpanID, ParentSpanID: "",
		ServiceName: rootSvc, Name: method + " " + route, Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(rootDur),
		StatusCode: statusCode, StatusMessage: statusMsg,
		Attributes: string(rootAttrs), Resource: string(rootResource),
		SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}
	child := &storage.Span{
		TraceID: traceID, SpanID: childSpanID, ParentSpanID: rootSpanID,
		ServiceName: "cart-svc", Name: "cart.v1.CartService/Load", Kind: 3,
		StartNs: startNs + int64(5*time.Millisecond), EndNs: startNs + int64(5*time.Millisecond+childDur),
		StatusCode: 1,
		Attributes: string(childAttrs), Resource: string(childResource),
		SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}
	dbSpan := &storage.Span{
		TraceID: traceID, SpanID: dbSpanID, ParentSpanID: childSpanID,
		ServiceName: "postgres", Name: "SELECT cart", Kind: 3,
		StartNs: startNs + int64(10*time.Millisecond), EndNs: startNs + int64(10*time.Millisecond+dbDur),
		StatusCode: 1,
		Attributes: string(dbAttrs), Resource: string(dbResource),
		SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}
	return []*storage.Span{root, child, dbSpan}
}

// buildN1TraceSpans constructs a parent with 12 nearly-identical DB child spans
// — the classic N+1 shape the detector triggers on.
func buildN1TraceSpans(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	root := &storage.Span{
		TraceID: traceID, SpanID: rootID, ParentSpanID: "",
		ServiceName: "checkout-api", Name: "GET /api/users", Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(600*time.Millisecond),
		StatusCode: 1,
		Attributes: `{"http.request.method":"GET","http.route":"/api/users","http.response.status_code":200}`,
		Resource:   `{"service.name":"checkout-api","deployment.environment":"seed"}`,
		SessionID:  sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}
	out := []*storage.Span{root}
	for i := 0; i < 12; i++ {
		spanID := deterministicID(sessionID+"-n1", "child", i, 16)
		child := &storage.Span{
			TraceID: traceID, SpanID: spanID, ParentSpanID: rootID,
			ServiceName: "postgres", Name: "SELECT users", Kind: 3,
			StartNs: startNs + int64(10*time.Millisecond) + int64(i)*int64(20*time.Millisecond),
			EndNs:   startNs + int64(10*time.Millisecond) + int64(i)*int64(20*time.Millisecond) + int64(18*time.Millisecond),
			StatusCode: 1,
			Attributes: fmt.Sprintf(`{"db.system":"postgresql","db.statement":"SELECT * FROM users WHERE id = %d"}`, i),
			Resource:   `{"service.name":"postgres"}`,
			SessionID:  sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
		}
		out = append(out, child)
	}

// buildSlowDBTrace returns a parent span containing a deliberately slow DB call.
func buildSlowDBTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	dbID := flipHex(rootID)
	dbAttrs := `{"db.system":"postgresql","db.statement":"SELECT * FROM reports WHERE date BETWEEN $1 AND $2","db.operation":"SELECT"}`
	resource := `{"service.name":"checkout-api","deployment.environment":"seed"}`
	return []*storage.Span{
		{TraceID: traceID, SpanID: rootID, ParentSpanID: "",
			ServiceName: "checkout-api", Name: "GET /api/reports", Kind: 2,
			StartNs: startNs, EndNs: startNs + int64(450*time.Millisecond),
			StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/reports"}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
		{TraceID: traceID, SpanID: dbID, ParentSpanID: rootID,
			ServiceName: "postgres", Name: "SELECT reports", Kind: 3,
			StartNs: startNs + int64(10*time.Millisecond), EndNs: startNs + int64(380*time.Millisecond),
			StatusCode: 1, Attributes: dbAttrs,
			Resource: `{"service.name":"postgres"}`, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
	}
}

// buildChattyHTTPTrace returns a trace with 6 calls to the same external host.
func buildChattyHTTPTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	resource := `{"service.name":"cart-svc","deployment.environment":"seed"}`
	spans := []*storage.Span{{
		TraceID: traceID, SpanID: rootID, ParentSpanID: "",
		ServiceName: "cart-svc", Name: "GET /api/items", Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(300*time.Millisecond),
		StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/items"}`,
		Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}}
	for i := 0; i < 6; i++ {
		cid := deterministicID(traceID+"-chatty", "call", i, 16)
		spans = append(spans, &storage.Span{
			TraceID: traceID, SpanID: cid, ParentSpanID: rootID,
			ServiceName: "cart-svc", Name: "GET /v1/price", Kind: 3,
			StartNs: startNs + int64(i)*int64(40*time.Millisecond), EndNs: startNs + int64(i)*int64(40*time.Millisecond) + int64(30*time.Millisecond),
			StatusCode: 1,
			Attributes: `{"url.full":"https://pricing.internal/v1/price","http.method":"GET","server.address":"pricing.internal"}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
		})
	}
	return spans
}

// buildTracingGapTrace returns a 500ms parent with children covering only 150ms,
// leaving a 350ms tracing gap.
func buildTracingGapTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	c1ID := flipHex(rootID)
	c2ID := flipHex(c1ID)
	resource := `{"service.name":"payments-svc","deployment.environment":"seed"}`
	return []*storage.Span{
		{TraceID: traceID, SpanID: rootID, ParentSpanID: "",
			ServiceName: "payments-svc", Name: "POST /api/charge", Kind: 2,
			StartNs: startNs, EndNs: startNs + int64(500*time.Millisecond),
			StatusCode: 1, Attributes: `{"http.method":"POST","http.route":"/api/charge"}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
		{TraceID: traceID, SpanID: c1ID, ParentSpanID: rootID,
			ServiceName: "payments-svc", Name: "validate card",
			StartNs: startNs + int64(10*time.Millisecond), EndNs: startNs + int64(80*time.Millisecond),
			StatusCode: 1, Attributes: `{}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
		{TraceID: traceID, SpanID: c2ID, ParentSpanID: rootID,
			ServiceName: "payments-svc", Name: "write audit log",
			StartNs: startNs + int64(430*time.Millisecond), EndNs: startNs + int64(490*time.Millisecond),
			StatusCode: 1, Attributes: `{}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
	}
}

// buildErrorChainTrace returns a trace where a child errors but the parent doesn't.
func buildErrorChainTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	childID := flipHex(rootID)
	resource := `{"service.name":"checkout-api","deployment.environment":"seed"}`
	return []*storage.Span{
		{TraceID: traceID, SpanID: rootID, ParentSpanID: "",
			ServiceName: "checkout-api", Name: "POST /api/checkout", Kind: 2,
			StartNs: startNs, EndNs: startNs + int64(200*time.Millisecond),
			StatusCode: 1, Attributes: `{"http.method":"POST","http.response.status_code":200}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
		{TraceID: traceID, SpanID: childID, ParentSpanID: rootID,
			ServiceName: "payments-svc", Name: "charge card",
			StartNs: startNs + int64(50*time.Millisecond), EndNs: startNs + int64(150*time.Millisecond),
			StatusCode: 2, StatusMessage: "card declined",
			Attributes: `{"rpc.system":"grpc","rpc.grpc.status_code":13}`,
			Resource: `{"service.name":"payments-svc"}`, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
	}
}

// buildSerialPromiseTrace returns a parent with 4 sequential client calls
// that could be issued in parallel.
func buildSerialPromiseTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	resource := `{"service.name":"checkout-api","deployment.environment":"seed"}`
	spans := []*storage.Span{{
		TraceID: traceID, SpanID: rootID, ParentSpanID: "",
		ServiceName: "checkout-api", Name: "GET /api/dashboard", Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(600*time.Millisecond),
		StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/dashboard"}`,
		Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}}
	services := []string{"users-svc", "orders-svc", "inventory-svc", "analytics-svc"}
	for i, svc := range services {
		cid := deterministicID(traceID+"-serial", "call", i, 16)
		offset := int64(i) * int64(120*time.Millisecond)
		spans = append(spans, &storage.Span{
			TraceID: traceID, SpanID: cid, ParentSpanID: rootID,
			ServiceName: "checkout-api", Name: "GET /" + svc, Kind: 3,
			StartNs: startNs + offset, EndNs: startNs + offset + int64(100*time.Millisecond),
			StatusCode: 1,
			Attributes: `{"http.method":"GET","server.address":"` + svc + `.internal"}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
		})
	}
	return spans
}

// buildSynchronousIOTrace returns a GET handler that makes 4 DB calls inline.
func buildSynchronousIOTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	resource := `{"service.name":"cart-svc","deployment.environment":"seed"}`
	spans := []*storage.Span{{
		TraceID: traceID, SpanID: rootID, ParentSpanID: "",
		ServiceName: "cart-svc", Name: "GET /api/cart/summary", Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(350*time.Millisecond),
		StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/cart/summary"}`,
		Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}}
	queries := []string{
		"SELECT * FROM cart WHERE session_id = $1",
		"SELECT * FROM items WHERE id = ANY($1)",
		"SELECT * FROM prices WHERE item_id = ANY($1)",
		"SELECT * FROM promotions WHERE active = true",
	}
	for i, q := range queries {
		cid := deterministicID(traceID+"-syncio", "db", i, 16)
		offset := int64(i) * int64(60*time.Millisecond)
		spans = append(spans, &storage.Span{
			TraceID: traceID, SpanID: cid, ParentSpanID: rootID,
			ServiceName: "postgres", Name: "SELECT", Kind: 3,
			StartNs: startNs + offset, EndNs: startNs + offset + int64(50*time.Millisecond),
			StatusCode: 1,
			Attributes: `{"db.system":"postgresql","db.statement":"` + q + `"}`,
			Resource: `{"service.name":"postgres"}`, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
		})
	}
	return spans
}

// buildLargePayloadTrace returns a span with a 3 MB response body attribute.
func buildLargePayloadTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	childID := flipHex(rootID)
	resource := `{"service.name":"checkout-api","deployment.environment":"seed"}`
	return []*storage.Span{
		{TraceID: traceID, SpanID: rootID, ParentSpanID: "",
			ServiceName: "checkout-api", Name: "GET /api/export", Kind: 2,
			StartNs: startNs, EndNs: startNs + int64(1200*time.Millisecond),
			StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/export","http.response.status_code":200}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
		{TraceID: traceID, SpanID: childID, ParentSpanID: rootID,
			ServiceName: "checkout-api", Name: "serialize response",
			StartNs: startNs + int64(100*time.Millisecond), EndNs: startNs + int64(1100*time.Millisecond),
			StatusCode: 1,
			Attributes: `{"http.response_content_length":3145728,"http.response.body.size":3145728}`,
			Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano()},
	}
}

// buildCacheMissStormTrace returns a parent with 12 Redis cache-miss spans
// clustered in a 25ms window.
func buildCacheMissStormTrace(traceID, sessionID, sessionLabel string, startNs int64) []*storage.Span {
	rootID := traceID[:16]
	resource := `{"service.name":"cart-svc","deployment.environment":"seed"}`
	spans := []*storage.Span{{
		TraceID: traceID, SpanID: rootID, ParentSpanID: "",
		ServiceName: "cart-svc", Name: "GET /api/recommendations", Kind: 2,
		StartNs: startNs, EndNs: startNs + int64(200*time.Millisecond),
		StatusCode: 1, Attributes: `{"http.method":"GET","http.route":"/api/recommendations"}`,
		Resource: resource, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
	}}
	for i := 0; i < 12; i++ {
		cid := deterministicID(traceID+"-cache", "miss", i, 16)
		offset := int64(i) * int64(2*time.Millisecond)
		spans = append(spans, &storage.Span{
			TraceID: traceID, SpanID: cid, ParentSpanID: rootID,
			ServiceName: "redis", Name: fmt.Sprintf("GET recommendation:%d", 1000+i),
			StartNs: startNs + int64(10*time.Millisecond) + offset,
			EndNs:   startNs + int64(10*time.Millisecond) + offset + int64(2*time.Millisecond),
			StatusCode: 1,
			Attributes: `{"db.system":"redis","cache.hit":false}`,
			Resource: `{"service.name":"redis"}`, SessionID: sessionID, SessionLabel: sessionLabel, ReceivedAt: time.Now().UnixNano(),
		})
	}
	return spans
}

// logsForSpan emits n logs whose trace_id AND span_id match the given span,
// so they surface in the inspector's per-span logs tab (/api/logs?spanId=…)
// at /traces/<trace_id>. Bodies are themed to the span's role.
func logsForSpan(rng *rand.Rand, sessionID string, sp *storage.Span, n int) []*storage.Log {
	infoBodies := []string{
		"handling " + sp.Name,
		"resolved route " + sp.Name,
		"cache hit for request",
		"authenticated user u_" + fmt.Sprint(1000+rng.IntN(9000)),
	}
	dbBodies := []string{
		"executing query on postgres",
		"acquired connection from pool (3/10 in use)",
		"query returned " + fmt.Sprint(rng.IntN(40)) + " rows in " + fmt.Sprint(5+rng.IntN(40)) + "ms",
	}
	pick := infoBodies
	if sp.ServiceName == "postgres" {
		pick = dbBodies
	}
	out := make([]*storage.Log, 0, n)
	for k := 0; k < n; k++ {
		attrs, _ := json.Marshal(map[string]any{
			"log.source":    "seed",
			"code.function": sp.Name,
			"thread.id":     1 + rng.IntN(8),
		})
		out = append(out, &storage.Log{
			TimestampNs: sp.StartNs + int64(k+1)*int64(2*time.Millisecond),
			TraceID:     sp.TraceID,
			SpanID:      sp.SpanID,
			Severity:    9, // INFO
			Body:        pick[rng.IntN(len(pick))],
			Attributes:  string(attrs),
			ServiceName: sp.ServiceName,
			SessionID:   sessionID,
			ReceivedAt:  time.Now().UnixNano(),
		})
	}
	return out
}

// errorLogForSpan emits a single ERROR log bound to a failed span.
func errorLogForSpan(sessionID string, sp *storage.Span) *storage.Log {
	attrs, _ := json.Marshal(map[string]any{
		"log.source":      "seed",
		"exception.type":  "UpstreamError",
		"http.status_code": 500,
	})
	return &storage.Log{
		TimestampNs: sp.EndNs - int64(time.Millisecond),
		TraceID:     sp.TraceID,
		SpanID:      sp.SpanID,
		Severity:    17, // ERROR
		Body:        "request failed: upstream returned 500 for " + sp.Name,
		Attributes:  string(attrs),
		ServiceName: sp.ServiceName,
		SessionID:   sessionID,
		ReceivedAt:  time.Now().UnixNano(),
	}
}

// freeFloatingLog is operational noise not tied to any trace — populates the
// global /logs view with non-request lines.
func freeFloatingLog(rng *rand.Rand, sessionID string, startNs int64, i int) *storage.Log {
	severities := []int{9, 9, 9, 13, 13, 17}
	bodies := []string{
		"reconnected to redis",
		"feature flag 'new-checkout' enabled",
		"config reloaded from disk",
		"background job 'reconcile' completed in 1.2s",
		"gc pause 14ms",
		"rate limiter: 0 requests throttled",
	}
	attrs, _ := json.Marshal(map[string]any{"log.source": "seed", "iter": i})
	return &storage.Log{
		TimestampNs: startNs + int64(i)*int64(3*time.Minute),
		Severity:    severities[rng.IntN(len(severities))],
		Body:        bodies[rng.IntN(len(bodies))],
		Attributes:  string(attrs),
		ServiceName: seedServices[i%len(seedServices)],
		SessionID:   sessionID,
		ReceivedAt:  time.Now().UnixNano(),
	}
}

// seedMetrics writes a counter, gauge, and histogram per service across a
// time-series of points. Returns the number of data points written.
func seedMetrics(store *storage.DB, sessionID string, startNs int64) (int, error) {
	const points = 20
	count := 0
	for _, svc := range seedServices {
		baseAttrs, _ := json.Marshal(map[string]any{"deployment.environment": "seed"})
		for i := 0; i < points; i++ {
			ts := startNs + int64(i)*int64(3*time.Minute)
			// http.server.duration as a histogram (one row per p50/p95/p99)
			for _, pct := range []struct {
				p   float64
				val float64
			}{{0.50, 45 + float64(i%5)*3}, {0.95, 180 + float64(i%5)*15}, {0.99, 320 + float64(i%5)*25}} {
				attrs, _ := json.Marshal(map[string]any{
					"deployment.environment": "seed",
					"percentile":             pct.p,
				})
				if err := store.InsertMetric(&storage.Metric{
					Name: "http.server.duration", Type: "histogram", Unit: "ms",
					TimestampNs: ts, Value: pct.val, Attributes: string(attrs),
					ServiceName: svc, SessionID: sessionID,
				}); err != nil {
					return count, err
				}
				count++
			}
			// requests counter (cumulative)
			if err := store.InsertMetric(&storage.Metric{
				Name: "http.server.requests", Type: "counter", Unit: "1",
				TimestampNs: ts, Value: float64(100 + i*7), Attributes: string(baseAttrs),
				ServiceName: svc, SessionID: sessionID,
			}); err != nil {
				return count, err
			}
			count++
			// open connections gauge
			if err := store.InsertMetric(&storage.Metric{
				Name: "process.open_connections", Type: "gauge", Unit: "1",
				TimestampNs: ts, Value: float64(8 + i%6), Attributes: string(baseAttrs),
				ServiceName: svc, SessionID: sessionID,
			}); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

// deterministicID returns the first n hex chars of sha1(kind + sessionID + index).
// Stable across runs so re-seeding produces identical IDs.
func deterministicID(salt, kind string, i int, n int) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%d", salt, kind, i)))
	full := hex.EncodeToString(h[:])
	if n > len(full) {
		n = len(full)
	}
	return full[:n]
}

func seedFromString(s string) uint64 {
	h := sha1.Sum([]byte(s))
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(h[i])
	}
	return v
}

// flipHex bit-flips every nibble so two derived IDs in the same call differ
// but stay deterministic.
func flipHex(s string) string {
	out := []byte(s)
	for i, b := range out {
		switch {
		case b >= '0' && b <= '9':
			out[i] = 'a' + (b-'0')%6
		case b >= 'a' && b <= 'f':
			out[i] = '0' + (b-'a')%10
		}
	}
	return string(out)
}
