package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// connect spins up the MCP handler over an httptest server and returns a
// connected client session plus the backing store.
func connect(t *testing.T, opts Options) (*mcpsdk.ClientSession, *storage.DB) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	sess, err := store.CreateSession("test-session", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	store.SetActiveSession(sess.ID, sess.Label)

	srv := httptest.NewServer(NewHandler(store, opts))
	t.Cleanup(srv.Close)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, store
}

func toolNames(t *testing.T, cs *mcpsdk.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestListToolsReadOnly(t *testing.T) {
	cs, _ := connect(t, Options{Version: "test-1.0", AllowWrites: false})
	names := toolNames(t, cs)
	if !names["get_server_info"] {
		t.Errorf("expected get_server_info to be registered, got %v", names)
	}
}

func TestHandlerHotReloadsWriteTools(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	handler := NewHandler(store, Options{Version: "test", AllowWrites: false})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	connectClient := func() *mcpsdk.ClientSession {
		client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1"}, nil)
		cs, err := client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}

	if names := toolNames(t, connectClient()); names["create_session"] {
		t.Fatal("create_session was exposed while writes were disabled")
	}

	handler.SetAllowWrites(true)
	writable := connectClient()
	if names := toolNames(t, writable); !names["create_session"] {
		t.Fatal("create_session was not exposed after enabling writes")
	}

	handler.SetAllowWrites(false)
	if _, err := writable.ListTools(context.Background(), nil); err == nil {
		t.Fatal("existing writable MCP session remained usable after disabling writes")
	}
	if names := toolNames(t, connectClient()); names["create_session"] {
		t.Fatal("create_session remained exposed after disabling writes")
	}
}

func TestGetServerInfo(t *testing.T) {
	cs, _ := connect(t, Options{Version: "test-1.0", AllowWrites: true})

	info := callStructured[ServerInfo](t, cs, "get_server_info", nil)

	if info.Version != "test-1.0" {
		t.Errorf("version = %q, want test-1.0", info.Version)
	}
	openAPIURL, err := url.Parse(info.OpenAPIURL)
	if err != nil || openAPIURL.Scheme != "http" || openAPIURL.Hostname() != "127.0.0.1" || openAPIURL.Port() == "" || openAPIURL.Path != "/api/openapi.json" {
		t.Errorf("openapi_url = %q, want absolute URL on the MCP request origin", info.OpenAPIURL)
	}
	if info.ActiveSessionLabel != "test-session" {
		t.Errorf("active session label = %q, want test-session", info.ActiveSessionLabel)
	}
	if !info.WritesEnabled {
		t.Errorf("writes_enabled = false, want true (AllowWrites was set)")
	}
}

func TestGetServerInfoPublicURL(t *testing.T) {
	cs, _ := connect(t, Options{PublicURL: "https://telemetry.example.com/spaniel/"})
	info := callStructured[ServerInfo](t, cs, "get_server_info", nil)
	if want := "https://telemetry.example.com/spaniel/api/openapi.json"; info.OpenAPIURL != want {
		t.Errorf("openapi_url = %q, want %q", info.OpenAPIURL, want)
	}
}

// The handler disables the SDK's DNS-rebinding guard, so a loopback request
// with a non-localhost Host header must be served (not rejected with 403).
// Spaniel is meant to be reachable beyond loopback; bearer auth is the gate.
func TestDNSRebindingProtectionDisabled(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := httptest.NewServer(NewHandler(store, Options{Version: "test"}))
	t.Cleanup(srv.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "evil.example.com" // non-localhost Host over a loopback connection
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("got 403 — DNS-rebinding guard is still active for a non-localhost Host")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDatabaseSizeLimitFollowsLivePolicy(t *testing.T) {
	limit := int64(123 << 20)
	cs, _ := connect(t, Options{MaxDBSizeBytes: func() int64 { return limit }})
	for _, want := range []int64{123 << 20, 0, 456 << 20} {
		limit = want
		if got := callStructured[IngestionHealthOutput](t, cs, "get_ingestion_health", nil).MaxDBSizeBytes; got != want {
			t.Fatalf("ingestion limit = %d, want %d", got, want)
		}
		if got := callStructured[ServerInfo](t, cs, "get_server_info", nil).MaxDBSizeBytes; got != want {
			t.Fatalf("server limit = %d, want %d", got, want)
		}
		if got := callStructured[StatsOutput](t, cs, "get_stats", nil).MaxDBSizeBytes; got != want {
			t.Fatalf("stats limit = %d, want %d", got, want)
		}
	}
}

func TestToolOutputSchemaAndStructuredContent(t *testing.T) {
	cs, _ := connect(t, Options{Version: "test", MaxDBSizeBytes: func() int64 { return 123456 }})
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range list.Tools {
		if tool.Name == "get_ingestion_health" {
			found = true
			if tool.OutputSchema == nil {
				t.Fatal("missing output schema")
			}
		}
	}
	if !found {
		t.Fatal("missing get_ingestion_health")
	}
	result, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "get_ingestion_health"})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil || len(result.Content) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out IngestionHealthOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.MaxDBSizeBytes != 123456 {
		t.Fatalf("structured limit = %d", out.MaxDBSizeBytes)
	}
	if _, ok := result.Content[0].(*mcpsdk.TextContent); !ok {
		t.Fatalf("missing compact text: %#v", result.Content)
	}
}

func TestIngestionHealthExplainsUnknownAndStoragePolicy(t *testing.T) {
	limit := int64(1 << 30)
	cs, _ := connect(t, Options{MaxDBSizeBytes: func() int64 { return limit }, AutoPrune: func() bool { return true }})
	out := callStructured[IngestionHealthOutput](t, cs, "get_ingestion_health", nil)
	if out.Status != "unknown" || len(out.Reasons) == 0 || out.EvaluatedAtNs == 0 {
		t.Fatalf("missing uncertainty: %#v", out)
	}
	if out.AutoPrune == nil || !*out.AutoPrune || out.StorageRemainingBytes == nil || *out.StorageRemainingBytes != limit-out.DBSizeBytes || out.StorageUtilizationPercent == nil {
		t.Fatalf("storage policy: %#v", out)
	}
}

type fixedHealthSources []storage.SourceStats

func (s fixedHealthSources) Sources() []storage.SourceStats { return s }
func TestIngestionHealthSourceFreshness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seen     int64
		rejected float64
		want     string
	}{
		{"fresh", time.Now().UnixNano(), 0, "healthy"},
		{"stale", time.Now().Add(-10 * time.Minute).UnixNano(), 0, "unknown"},
		{"rejected", time.Now().UnixNano(), 1, "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, _ := connect(t, Options{Sources: fixedHealthSources{{LastSeenNs: tc.seen, RejectedPerSec: tc.rejected}}})
			out := callStructured[IngestionHealthOutput](t, cs, "get_ingestion_health", nil)
			if out.Status != tc.want || out.LastSourceSeenNs != tc.seen {
				t.Fatalf("status = %#v, want %s", out, tc.want)
			}
		})
	}
}
