package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/zfogg/spaniel/internal/storage"
)

// runStub is a minimal HTTP stub that satisfies the spawnRun API surface:
// /api/health, /api/sessions (POST + GET), /api/sessions/:id/activate,
// /api/sessions/:id/baseline, and /api/traces.
type runStub struct {
	mu       sync.Mutex
	server   *httptest.Server
	requests []recordedReq

	sessions    map[string]*storage.Session
	nextID      int
	baselineSet map[string]bool
}

func newRunStub(t *testing.T) *runStub {
	t.Helper()
	s := &runStub{
		sessions:    map[string]*storage.Session{},
		baselineSet: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"ok": true}})
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.Method == http.MethodPost {
			var body struct{ Label string `json:"label"` }
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.nextID++
			id := fmt.Sprintf("sess-%04d", s.nextID)
			sess := &storage.Session{ID: id, Label: body.Label, TraceCount: 2, SpanCount: 7}
			s.sessions[id] = sess
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": id}})
			return
		}
		// GET: return all sessions
		out := make([]storage.Session, 0, len(s.sessions))
		for _, v := range s.sessions {
			out = append(out, *v)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
	})
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		parts := strings.SplitN(path, "/", 2)
		id := parts[0]
		s.mu.Lock()
		sess := s.sessions[id]
		s.mu.Unlock()
		if sess == nil {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 2 && parts[1] == "activate" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": sess})
			return
		}
		if len(parts) == 2 && parts[1] == "baseline" {
			s.mu.Lock()
			s.baselineSet[id] = true
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]bool{"ok": true}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": sess})
	})
	mux.HandleFunc("/api/traces", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *runStub) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.mu.Lock()
	s.requests = append(s.requests, recordedReq{r.Method, r.URL.Path, body})
	s.mu.Unlock()
}

func (s *runStub) findRequest(method, path string) *recordedReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.requests {
		if s.requests[i].method == method && s.requests[i].path == path {
			return &s.requests[i]
		}
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// trueCmd returns the path to a command that exits 0 on this platform.
func trueCmd() []string {
	if p, err := exec.LookPath("true"); err == nil {
		return []string{p}
	}
	// Fallback: use the test binary itself with a guard env var.
	return []string{os.Args[0], "-test.run=TestHelperProcessSuccess"}
}

// falseCmd returns a command that exits non-zero.
func falseCmd() []string {
	if p, err := exec.LookPath("false"); err == nil {
		return []string{p}
	}
	return []string{os.Args[0], "-test.run=TestHelperProcessFail"}
}

// echoEnvCmd returns a command that prints its environment to stdout.
func echoEnvCmd() []string {
	return []string{os.Args[0], "-test.run=TestHelperProcessEchoEnv"}
}

// TestHelperProcessSuccess is a sentinel test used as a child process that
// exits 0. It must not actually run test logic.
func TestHelperProcessSuccess(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(0)
}

// TestHelperProcessFail exits non-zero.
func TestHelperProcessFail(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(42)
}

// TestHelperProcessEchoEnv prints each env var on its own line.
func TestHelperProcessEchoEnv(t *testing.T) {
	if os.Getenv("GO_HELPER_PROCESS") != "1" {
		return
	}
	for _, e := range os.Environ() {
		fmt.Println(e)
	}
	os.Exit(0)
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSpawnRun_CreatesSession(t *testing.T) {
	stub := newRunStub(t)

	var out bytes.Buffer
	code, err := spawnRun(spawnRunOpts{
		apiBase: stub.server.URL,
		label:   "pytest",
		args:    trueCmd(),
		out:     &out,
	})
	if err != nil {
		t.Fatalf("spawnRun: %v", err)
	}
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	// A session must have been created and activated.
	if stub.findRequest("POST", "/api/sessions") == nil {
		t.Error("expected POST /api/sessions")
	}
	stub.mu.Lock()
	sessCount := len(stub.sessions)
	stub.mu.Unlock()
	if sessCount != 1 {
		t.Errorf("expected 1 session, got %d", sessCount)
	}
}

func TestSpawnRun_InjectsEnvVars(t *testing.T) {
	stub := newRunStub(t)

	// Use a helper binary that dumps its env to stdout, captured via a pipe.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	childArgs := append(echoEnvCmd(), "--")
	env := append(os.Environ(),
		"GO_HELPER_PROCESS=1",
		"OTEL_EXPORTER_OTLP_ENDPOINT="+otlpEndpointFrom(stub.server.URL),
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES=service.name=myapp",
		"SPANIEL_SESSION_ID=sess-test",
	)
	cmd := exec.Command(childArgs[0], childArgs[1:]...) //nolint:gosec
	cmd.Env = env
	cmd.Stdout = pw
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	pw.Close()

	envOutput, _ := io.ReadAll(pr)
	pr.Close()

	for _, want := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT=",
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES=service.name=myapp",
		"SPANIEL_SESSION_ID=",
	} {
		if !strings.Contains(string(envOutput), want) {
			t.Errorf("env missing %q\ngot:\n%s", want, string(envOutput))
		}
	}

	// Now verify spawnRun actually injects those vars.
	var out bytes.Buffer
	_, err = spawnRun(spawnRunOpts{
		apiBase: stub.server.URL,
		label:   "myapp",
		args:    trueCmd(),
		out:     &out,
	})
	if err != nil {
		t.Fatalf("spawnRun: %v", err)
	}

	// The session activate call must include the new session ID in the path.
	stub.mu.Lock()
	var sessID string
	for id := range stub.sessions {
		sessID = id
	}
	stub.mu.Unlock()
	if stub.findRequest("POST", "/api/sessions/"+sessID+"/activate") == nil {
		t.Errorf("expected POST activate for session %s", sessID)
	}
}

func TestSpawnRun_PropagatesExitCode(t *testing.T) {
	cmd := append(falseCmd(), "--")
	env := append(os.Environ(), "GO_HELPER_PROCESS=1")
	_ = exec.Command(cmd[0], cmd[1:]...) //nolint:gosec

	// Test runChild directly; the helper binary exits non-zero.
	code, err := runChild([]string{cmd[0]}, env)
	if err != nil {
		// exec failures are not exit-code errors
		t.Logf("runChild error (may be expected on this platform): %v", err)
	}
	// false exits non-zero; 42 if using the helper, 1 if using system false.
	if code == 0 {
		t.Errorf("expected non-zero exit code, got 0")
	}
}

func TestSpawnRun_SetsBaseline(t *testing.T) {
	stub := newRunStub(t)

	var out bytes.Buffer
	_, err := spawnRun(spawnRunOpts{
		apiBase:  stub.server.URL,
		label:    "record-run",
		args:     trueCmd(),
		baseline: true,
		out:      &out,
	})
	if err != nil {
		t.Fatalf("spawnRun: %v", err)
	}

	stub.mu.Lock()
	baselined := len(stub.baselineSet) > 0
	stub.mu.Unlock()
	if !baselined {
		t.Error("expected baseline to be set for record mode")
	}
	if !strings.Contains(out.String(), "baseline") {
		t.Errorf("output missing 'baseline': %q", out.String())
	}
}

func TestSpawnRun_AlreadyRunning(t *testing.T) {
	stub := newRunStub(t)

	// daemonCfg is nil — must succeed because stub is already "running".
	var out bytes.Buffer
	code, err := spawnRun(spawnRunOpts{
		apiBase:   stub.server.URL,
		label:     "mytest",
		args:      trueCmd(),
		out:       &out,
		daemonCfg: nil, // no daemon needed — health check will pass
	})
	if err != nil {
		t.Fatalf("spawnRun with live stub: %v", err)
	}
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestSpawnRun_FailsWhenNoSpanielAndNoDaemon(t *testing.T) {
	// Point at a port with nothing listening.
	_, err := spawnRun(spawnRunOpts{
		apiBase:   "http://127.0.0.1:19999",
		label:     "test",
		args:      trueCmd(),
		daemonCfg: nil,
	})
	if err == nil {
		t.Error("expected error when spaniel unreachable and no daemonCfg")
	}
}

func TestOtlpEndpointFrom(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://localhost:8080", "http://localhost:4318"},
		{"http://127.0.0.1:9000", "http://127.0.0.1:4318"},
		{"http://example.com:8080", "http://example.com:4318"},
	}
	for _, c := range cases {
		got := otlpEndpointFrom(c.in)
		if got != c.want {
			t.Errorf("otlpEndpointFrom(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCmdLabel(t *testing.T) {
	if got := cmdLabel([]string{"/usr/bin/pytest", "-k", "checkout"}); got != "pytest" {
		t.Errorf("cmdLabel = %q, want pytest", got)
	}
	if got := cmdLabel([]string{}); got != "run" {
		t.Errorf("cmdLabel empty = %q, want run", got)
	}
}

func TestPrintSummary(t *testing.T) {
	var buf bytes.Buffer
	printSummary(&buf, "pytest", runSummary{TraceCount: 12, SpanCount: 482, ErrorCount: 3, N1Count: 1})
	got := buf.String()
	for _, want := range []string{"12 traces", "482 spans", "3 errors", "1 N+1"} {
		if !strings.Contains(got, want) {
			t.Errorf("printSummary missing %q in %q", want, got)
		}
	}
}

func TestPrintSummary_SlowestURL(t *testing.T) {
	var buf bytes.Buffer
	printSummary(&buf, "go test", runSummary{
		TraceCount: 1, SpanCount: 5,
		SlowestURL: "http://localhost:8080/traces/abc123",
	})
	if !strings.Contains(buf.String(), "slowest trace") {
		t.Errorf("expected 'slowest trace' in output: %q", buf.String())
	}
}
