package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/receiver"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func TestPhysicalPruningKeepsDirectOTLPIngestionAvailable(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "direct.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession("pruning", false)
	if err != nil {
		t.Fatal(err)
	}
	db.SetActiveSession(session.ID, session.Label)
	for i := range 1800 {
		payload := make([]byte, 4096)
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		if err := db.AppendSpan(&storage.Span{SessionID: session.ID, TraceID: fmt.Sprint(i), SpanID: fmt.Sprint(i), StartNs: int64(i + 1), EndNs: int64(i + 2), Attributes: `{"payload":"` + hex.EncodeToString(payload) + `"}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	cap := db.FileSize() * 11 / 10
	policy := newStorageGuardPolicy(cap, true)
	pipeline := ingestion.NewPipeline(db, ws.NewHub())
	defer pipeline.Close(context.Background())
	r := receiver.NewHTTPReceiver(pipeline)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", r.HandleTraces)
	mux.HandleFunc("/v1/logs", r.HandleLogs)
	mux.HandleFunc("/v1/metrics", r.HandleMetrics)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	accepted := 0
	var maxLatency time.Duration
	for phase := range 12 {
		if phase > 0 && db.FileSize() <= cap*storagePruneTargetPercent/100 {
			break
		}
		policy.nextPruneAfter.Store(0)
		done := make(chan struct{})
		go func() {
			defer close(done)
			checkStorageGuard(db, policy)
		}()
		// Requests use the real receiver and acknowledgment/flush pipeline while
		// the real file is replaced, with no collector or retry proxy in between.
		at := time.Now().UnixNano()
		for signal, payload := range map[string]string{
			"traces":  fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"direct-prune"}}]},"scopeSpans":[{"spans":[{"traceId":"%032x","spanId":"%016x","name":"newest","startTimeUnixNano":"%d","endTimeUnixNano":"%d"}]}]}]}`, phase+10000, phase+10000, at, at+1),
			"logs":    fmt.Sprintf(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"%d","body":{"stringValue":"newest-prune-probe"}}]}]}]}`, at),
			"metrics": fmt.Sprintf(`{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{"name":"newest_prune_probe","gauge":{"dataPoints":[{"timeUnixNano":"%d","asDouble":1}]}}]}]}]}`, at),
		} {
			started := time.Now()
			resp, err := client.Post(server.URL+"/v1/"+signal, "application/json", bytes.NewBufferString(payload))
			maxLatency = max(maxLatency, time.Since(started))
			if err != nil {
				<-done
				t.Fatalf("phase=%d signal=%s: %v", phase, signal, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				<-done
				t.Fatalf("phase=%d signal=%s status=%d body=%s", phase, signal, resp.StatusCode, body)
			}
			accepted++
		}
		<-done
		if db.Full() {
			t.Fatal("proactive phase acquired the hard-cap admission latch")
		}
	}
	if db.FileSize() > cap*storagePruneTargetPercent/100 {
		t.Fatalf("pruning never reached physical low water: size=%d cap=%d", db.FileSize(), cap)
	}
	var newest int
	if err := db.WithContext(context.Background()).SQL().QueryRow("SELECT count(*) FROM spans WHERE name='newest'").Scan(&newest); err != nil || newest == 0 {
		t.Fatalf("newest accepted spans lost: count=%d err=%v", newest, err)
	}
	t.Logf("direct accepted=%d max_ack_latency=%s physical_bytes=%d cap=%d", accepted, maxLatency, db.FileSize(), cap)
}
