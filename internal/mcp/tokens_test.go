package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zfogg/spaniel/internal/security"
	"github.com/zfogg/spaniel/internal/storage"
)

type tokenTransport struct{ secret string }

func (t tokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.secret)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCPTokenToolsAndAuthenticatedSQL(t *testing.T) {
	private, err := security.Open(":memory:", "example-encryption-password")
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	admin, err := private.Create(context.Background(), security.CreateInput{Name: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err = private.Configure(true, "", nil); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "telemetry.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := NewHandler(store, Options{Security: private, AllowSQLWrites: true})
	srv := httptest.NewServer(private.HTTP(h, false))
	defer srv.Close()
	connect := func(secret string) (*mcpsdk.ClientSession, error) {
		client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "token-test", Version: "1"}, nil)
		return client.Connect(context.Background(), &mcpsdk.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: tokenTransport{secret}}}, nil)
	}
	if client, err := connect("wrong"); err == nil {
		_ = client.Close()
		t.Fatal("MCP accepted unauthenticated initialization")
	}
	client, err := connect(admin.Secret)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	names := toolNames(t, client)
	if !names["list_tokens"] || names["create_token"] || names["delete_token"] {
		t.Fatal("token tools ignored MCP write gate", names)
	}
	result, err := client.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "query_sql", Arguments: map[string]any{"sql": "SELECT * FROM api_tokens"}})
	if err == nil && !result.IsError {
		t.Fatal("MCP SQL read private tokens")
	}
	h.SetAllowWrites(true)
	_ = client.Close()
	client, err = connect(admin.Secret)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	created := callStructured[security.Created](t, client, "create_token", map[string]any{"name": "collector", "no_expiry": true})
	if created.Secret == "" {
		t.Fatal("create_token did not disclose secret")
	}
	listed, err := client.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_tokens"})
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range listed.Content {
		if text, ok := content.(*mcpsdk.TextContent); ok && strings.Contains(text.Text, created.Secret) {
			t.Fatal("list_tokens leaked secret")
		}
	}
	_ = callStructured[map[string]bool](t, client, "delete_token", map[string]any{"id": created.Token.ID})
	if _, code := private.Validate(created.Secret); code != "token_revoked" {
		t.Fatal(code)
	}
	// An existing MCP session ID is not an authentication credential.
	if err = private.Delete(context.Background(), admin.Token.ID); err == nil {
		t.Fatal("deleted final token")
	}
	_, err = private.Create(context.Background(), security.CreateInput{Name: "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	if err = private.Delete(context.Background(), admin.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = client.ListTools(context.Background(), nil); err == nil {
		t.Fatal("revoked token retained MCP session access")
	}
}
