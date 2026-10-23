package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/zfogg/spaniel/internal/storage"
)

// Final self exports must reach the coordinator before it closes. Closing it
// first returns retryable UNAVAILABLE to our own metrics exporter and can
// leave the process retrying until Docker force-kills it.
func TestRunSelfMonitoringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	oldNotify := notifyShutdown
	notifyShutdown = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) { return ctx, func() {} }
	t.Cleanup(func() { notifyShutdown = oldNotify; cancel() })
	freePort := func() int {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		return port
	}
	port, grpcPort, httpPort := freePort(), freePort(), freePort()
	path := filepath.Join(t.TempDir(), "self-shutdown.duckdb")
	cfg := runConfig{
		Viper: viper.New(),
		Port:  port, DBPath: path, NoBrowser: true, BindAddressV4: "127.0.0.1",
		OTLPGRPCPort: grpcPort, OTLPHTTPPort: httpPort,
		SelfTelemetryEndpoint: fmt.Sprintf("127.0.0.1:%d", grpcPort),
		SelfTelemetryService:  "shutdown-self-test", SelfTelemetryInsecure: true,
		SampleAlwaysKeep: "error",
	}
	done := make(chan error, 1)
	go func() { done <- run(cfg) }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", port))
		if err == nil {
			_ = response.Body.Close()
			ready = response.StatusCode == http.StatusOK
			if ready {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatal("self-monitoring collector never became healthy")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("self export shutdown retried against a closed ingestion queue")
	}
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var metrics int
	if err := db.SQL().QueryRow("SELECT count(*) FROM metrics WHERE service_name = ?", cfg.SelfTelemetryService).Scan(&metrics); err != nil {
		t.Fatal(err)
	}
	if metrics == 0 {
		t.Fatal("final self metrics were not persisted before shutdown")
	}
}
