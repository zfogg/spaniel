package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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

func TestGetServerInfo(t *testing.T) {
	cs, _ := connect(t, Options{Version: "test-1.0", AllowWrites: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "get_server_info"})
	if err != nil {
		t.Fatalf("call get_server_info: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_server_info returned tool error: %+v", res.Content)
	}

	// StructuredContent round-trips as a generic map; re-decode into ServerInfo.
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var info ServerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("unmarshal ServerInfo: %v", err)
	}

	if info.Version != "test-1.0" {
		t.Errorf("version = %q, want test-1.0", info.Version)
	}
	if info.ActiveSessionLabel != "test-session" {
		t.Errorf("active session label = %q, want test-session", info.ActiveSessionLabel)
	}
	if !info.WritesEnabled {
		t.Errorf("writes_enabled = false, want true (AllowWrites was set)")
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
