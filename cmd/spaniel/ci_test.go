package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/ci"
)

// stubServer returns an httptest.Server that serves a fixed active session
// and a fixed snapshot from the baseline-export endpoint.
func stubServer(t *testing.T, sessionID string, snap ci.Snapshot) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/active", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]string{"id": sessionID, "label": "active-test"},
		})
	})
	mux.HandleFunc("/api/sessions/"+sessionID+"/baseline-export", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": snap})
	})
	return httptest.NewServer(mux)
}

func TestCIExport_WritesSnapshotFile(t *testing.T) {
	snap := ci.Snapshot{
		Version:        ci.SnapshotVersion,
		SessionLabel:   "test-session",
		RootDurationNs: 12345,
		Operations: []ci.Operation{
			{Service: "api", Name: "GET /cart", Count: 1, P95Ns: 100},
		},
	}
	srv := stubServer(t, "sess-1", snap)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "baseline.json")
	if err := ciExport(srv.URL, "", out); err != nil {
		t.Fatalf("ciExport: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var got ci.Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.SessionLabel != "test-session" || got.RootDurationNs != 12345 {
		t.Errorf("snapshot round-trip wrong: %+v", got)
	}
	if len(got.Operations) != 1 || got.Operations[0].Name != "GET /cart" {
		t.Errorf("operations missing: %+v", got.Operations)
	}
}

func TestCIExport_ExplicitSession(t *testing.T) {
	// When --session is supplied, ciExport should NOT call /api/sessions/active.
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/active", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	})
	mux.HandleFunc("/api/sessions/explicit-sess/baseline-export", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": ci.Snapshot{Version: ci.SnapshotVersion}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "baseline.json")
	if err := ciExport(srv.URL, "explicit-sess", out); err != nil {
		t.Fatalf("ciExport: %v", err)
	}
	if called {
		t.Errorf("/api/sessions/active should NOT be called when --session is set")
	}
}

func TestCIExport_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/active") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "s"}})
			return
		}
		http.Error(w, `{"error":"boom"}`, 500)
	}))
	defer srv.Close()

	err := ciExport(srv.URL, "", filepath.Join(t.TempDir(), "x.json"))
	if err == nil {
		t.Fatal("expected error from upstream 500, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 in error, got %v", err)
	}
}

// runCheckCapture invokes ciCheck and captures (regressionsFound, error).
// We can't easily test the os.Exit(1) path, so we observe the printed
// "regression(s) detected" line on stdout when --json=false.
func runCheckCapture(t *testing.T, fn func() error) (stdout string, err error) {
	t.Helper()
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var sb strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, e := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if e != nil {
				break
			}
		}
		close(done)
	}()

	// Save and restore so a Compare regression doesn't kill the test binary.
	defer func() {
		_ = w.Close()
		<-done
		stdout = sb.String()
	}()

	err = fn()
	return
}

func writeBaseline(t *testing.T, snap ci.Snapshot) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.json")
	data, _ := json.Marshal(snap)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCICheck_PassWhenSnapshotsEqual(t *testing.T) {
	snap := ci.Snapshot{
		Version:    ci.SnapshotVersion,
		Operations: []ci.Operation{{Service: "api", Name: "GET /", P95Ns: 100}},
	}
	srv := stubServer(t, "s", snap)
	defer srv.Close()
	baseline := writeBaseline(t, snap)

	out, err := runCheckCapture(t, func() error {
		return ciCheck(srv.URL, baseline, "", ci.DefaultCheckOptions(), false)
	})
	if err != nil {
		t.Fatalf("ciCheck: %v", err)
	}
	if !strings.Contains(out, "no regressions detected") {
		t.Errorf("expected pass message in stdout, got: %q", out)
	}
}

func TestCICheck_JSONOutput_PassFlag(t *testing.T) {
	snap := ci.Snapshot{Version: ci.SnapshotVersion}
	srv := stubServer(t, "s", snap)
	defer srv.Close()
	baseline := writeBaseline(t, snap)

	out, err := runCheckCapture(t, func() error {
		return ciCheck(srv.URL, baseline, "", ci.DefaultCheckOptions(), true)
	})
	if err != nil {
		t.Fatalf("ciCheck: %v", err)
	}
	var parsed ci.Result
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		t.Fatalf("expected JSON output, got %q (err: %v)", out, jerr)
	}
	if !parsed.Pass {
		t.Errorf("expected pass=true, got %+v", parsed)
	}
}

func TestCICheck_MissingBaselineFile(t *testing.T) {
	srv := stubServer(t, "s", ci.Snapshot{Version: ci.SnapshotVersion})
	defer srv.Close()

	err := ciCheck(srv.URL, "/no/such/file.json", "", ci.DefaultCheckOptions(), false)
	if err == nil {
		t.Fatal("expected error for missing baseline file")
	}
}

func TestActiveSessionID_NoActiveSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": ""}})
	}))
	defer srv.Close()

	_, err := activeSessionID(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "no active session") {
		t.Errorf("expected 'no active session' error, got %v", err)
	}
}
