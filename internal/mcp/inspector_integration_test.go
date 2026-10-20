package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

const inspectorPackage = "@modelcontextprotocol/inspector@2.11.0"

// TestMCPInspector exercises Spaniel through the reference MCP Inspector CLI,
// across the real Streamable HTTP boundary. It is opt-in because npx may need
// network access to populate its package cache; CI and make
// test-mcp-e2e enable it explicitly.
func TestMCPInspector(t *testing.T) {
	if os.Getenv("SPANIEL_MCP_INSPECTOR_INTEGRATION") != "1" {
		t.Skip("set SPANIEL_MCP_INSPECTOR_INTEGRATION=1 or run make test-mcp-e2e")
	}
	requireInspectorRuntime(t)

	store, err := storage.Open(filepath.Join(t.TempDir(), "inspector.duckdb"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	session, err := store.CreateSession("inspector-session", false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	store.SetActiveSession(session.ID, session.Label)

	mux := http.NewServeMux()
	mux.Handle("/mcp", NewHandler(store, Options{Version: "inspector-integration"}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	endpoint := srv.URL + "/mcp"

	t.Run("initialize", func(t *testing.T) {
		result := inspectorResult(t, endpoint, "--method", "initialize")
		serverInfo := objectField(t, result, "serverInfo")
		if got := stringField(t, serverInfo, "name"); got != "spaniel" {
			t.Fatalf("serverInfo.name = %q, want spaniel", got)
		}
		if got := stringField(t, serverInfo, "version"); got != "inspector-integration" {
			t.Fatalf("serverInfo.version = %q, want inspector-integration", got)
		}
	})

	t.Run("tools", func(t *testing.T) {
		result := inspectorResult(t, endpoint, "--method", "tools/list", "--strict")
		names := namedItems(t, result, "tools")
		for _, want := range []string{"get_server_info", "investigate_incident", "compare_session_to_baseline", "draft_observability_pack"} {
			if !names[want] {
				t.Errorf("tools/list omitted %q", want)
			}
		}
		if names["create_session"] {
			t.Error("read-only Inspector server exposed create_session")
		}
	})

	t.Run("tool call returns TOON", func(t *testing.T) {
		result := inspectorResult(t, endpoint,
			"--method", "tools/call",
			"--tool-name", "get_server_info",
			"--tool-args-json", `{}`,
		)
		text := firstContentText(t, result)
		for _, want := range []string{"version: inspector-integration", "active_session_label: inspector-session", "writes_enabled: false"} {
			if !strings.Contains(text, want) {
				t.Errorf("TOON tool result missing %q:\n%s", want, text)
			}
		}
		if _, exists := result["structuredContent"]; exists {
			t.Error("tool response unexpectedly duplicated TOON as structuredContent")
		}
	})

	t.Run("prompts", func(t *testing.T) {
		result := inspectorResult(t, endpoint, "--method", "prompts/list")
		names := namedItems(t, result, "prompts")
		for _, want := range []string{"investigate_incident", "compare_session_to_baseline", "draft_observability_pack"} {
			if !names[want] {
				t.Errorf("prompts/list omitted %q", want)
			}
		}

		result = inspectorResult(t, endpoint,
			"--method", "prompts/get",
			"--prompt-name", "compare_session_to_baseline",
			"--prompt-args", "baseline=before",
			"--prompt-args", "compare=after",
		)
		messages := arrayField(t, result, "messages")
		if len(messages) == 0 {
			t.Fatal("prompts/get returned no messages")
		}
		message, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("prompt message has type %T", messages[0])
		}
		content := objectField(t, message, "content")
		text := stringField(t, content, "text")
		for _, want := range []string{`use baseline "before"`, `use comparison session "after"`} {
			if !strings.Contains(text, want) {
				t.Errorf("prompt result missing %q:\n%s", want, text)
			}
		}
	})

	t.Run("resources", func(t *testing.T) {
		result := inspectorResult(t, endpoint, "--method", "resources/list")
		uris := uriItems(t, result, "resources")
		for _, want := range []string{schemaURI, guideURI} {
			if !uris[want] {
				t.Errorf("resources/list omitted %q", want)
			}
		}

		result = inspectorResult(t, endpoint, "--method", "resources/templates/list")
		if got := len(arrayField(t, result, "resourceTemplates")); got < 5 {
			t.Errorf("resources/templates/list returned %d templates, want at least 5", got)
		}
	})
}

func requireInspectorRuntime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npx"); err != nil {
		t.Fatalf("npx is required for %s: %v", inspectorPackage, err)
	}
	cmd := exec.Command("node", "--version")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Node.js 22.19.0 or newer is required: %v", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "v%d.%d", &major, &minor); err != nil {
		t.Fatalf("parse Node.js version %q: %v", strings.TrimSpace(string(out)), err)
	}
	if major < 22 || major == 22 && minor < 19 {
		t.Fatalf("Node.js %d.%d is too old; MCP Inspector requires 22.19.0 or newer", major, minor)
	}
}

func inspectorResult(t *testing.T, endpoint string, args ...string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	commandArgs := []string{"-y", inspectorPackage, "--cli", endpoint, "--transport", "http", "--stored-auth-only"}
	commandArgs = append(commandArgs, args...)
	commandArgs = append(commandArgs, "--format", "json")

	var stdout, stderr bytes.Buffer
	cmdName := "npx"
	if runtime.GOOS == "windows" {
		cmdName = "npx.cmd"
	}
	cmd := exec.CommandContext(ctx, cmdName, commandArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Inspector %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}

	var envelope struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode Inspector JSON for %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	if envelope.Result == nil {
		t.Fatalf("Inspector %v returned no result:\n%s", args, stdout.String())
	}
	return envelope.Result
}

func objectField(t *testing.T, object map[string]any, field string) map[string]any {
	t.Helper()
	value, ok := object[field].(map[string]any)
	if !ok {
		t.Fatalf("field %q has type %T, want object", field, object[field])
	}
	return value
}

func arrayField(t *testing.T, object map[string]any, field string) []any {
	t.Helper()
	value, ok := object[field].([]any)
	if !ok {
		t.Fatalf("field %q has type %T, want array", field, object[field])
	}
	return value
}

func stringField(t *testing.T, object map[string]any, field string) string {
	t.Helper()
	value, ok := object[field].(string)
	if !ok {
		t.Fatalf("field %q has type %T, want string", field, object[field])
	}
	return value
}

func namedItems(t *testing.T, result map[string]any, field string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, raw := range arrayField(t, result, field) {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s item has type %T", field, raw)
		}
		names[stringField(t, item, "name")] = true
	}
	return names
}

func uriItems(t *testing.T, result map[string]any, field string) map[string]bool {
	t.Helper()
	uris := map[string]bool{}
	for _, raw := range arrayField(t, result, field) {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s item has type %T", field, raw)
		}
		uris[stringField(t, item, "uri")] = true
	}
	return uris
}

func firstContentText(t *testing.T, result map[string]any) string {
	t.Helper()
	content := arrayField(t, result, "content")
	if len(content) == 0 {
		t.Fatal("tool response contained no content")
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("tool content has type %T", content[0])
	}
	return stringField(t, item, "text")
}
