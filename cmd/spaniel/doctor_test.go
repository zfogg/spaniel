package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── checkConfigFile ──────────────────────────────────────────────────────────

func TestCheckConfigFile_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := checkConfigFile(path)
	if r.Status != checkOK {
		t.Errorf("status = %d, want OK; detail=%q hint=%q", r.Status, r.Detail, r.Hint)
	}
}

func TestCheckConfigFile_MissingIsWarning(t *testing.T) {
	r := checkConfigFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if r.Status != checkWarn {
		t.Errorf("expected warn for missing config, got %d", r.Status)
	}
	if !strings.Contains(r.Detail, "missing") {
		t.Errorf("detail should mention 'missing', got %q", r.Detail)
	}
}

func TestCheckConfigFile_DirectoryIsFailure(t *testing.T) {
	r := checkConfigFile(t.TempDir())
	if r.Status != checkFail {
		t.Errorf("expected fail when path is a dir, got %d", r.Status)
	}
}

// ── checkDBWritable ──────────────────────────────────────────────────────────

func TestCheckDBWritable_OK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spaniel.duckdb")
	r := checkDBWritable(path)
	if r.Status != checkOK {
		t.Fatalf("expected OK, got %d (hint=%q)", r.Status, r.Hint)
	}
	if !strings.Contains(r.Detail, "spaniel.duckdb") {
		t.Errorf("detail should include the path, got %q", r.Detail)
	}
}

func TestCheckDBWritable_UnsetFails(t *testing.T) {
	r := checkDBWritable("")
	if r.Status != checkFail {
		t.Errorf("expected fail when db_path is empty, got %d", r.Status)
	}
}

// ── checkPort ────────────────────────────────────────────────────────────────

func TestCheckPort_Free(t *testing.T) {
	// Find a port nobody's using, immediately close it, then run the check.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	r := checkPort("test", port)
	if r.Status != checkOK {
		t.Errorf("expected OK for free port %d, got %d (hint=%q)", port, r.Status, r.Hint)
	}
}

func TestCheckPort_AlreadyBoundFails(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	port := ln.Addr().(*net.TCPAddr).Port

	r := checkPort("test", port)
	if r.Status != checkFail {
		t.Fatalf("expected FAIL for occupied port %d, got %d", port, r.Status)
	}
	if !strings.Contains(r.Detail, fmt.Sprintf(":%d", port)) {
		t.Errorf("detail should mention port number, got %q", r.Detail)
	}
}

func TestCheckPort_ZeroIsDisabled(t *testing.T) {
	r := checkPort("opt-out", 0)
	if r.Status != checkWarn {
		t.Errorf("expected warn for port 0 (disabled), got %d", r.Status)
	}
}

func TestCheckPort_OutOfRangeFails(t *testing.T) {
	for _, p := range []int{-1, 70000} {
		r := checkPort("bad", p)
		if r.Status != checkFail {
			t.Errorf("port %d should fail, got status %d", p, r.Status)
		}
	}
}

// ── checkFrontendEmbed ───────────────────────────────────────────────────────

func TestCheckFrontendEmbed_Reports(t *testing.T) {
	// Depending on whether `pnpm build` ran before `go build`, this can be
	// either OK (full bundle) or FAIL (placeholder only). Either is a valid
	// signal; just assert it gives ONE of those statuses and a useful detail.
	r := checkFrontendEmbed()
	if r.Status != checkOK && r.Status != checkFail {
		t.Errorf("unexpected status %d", r.Status)
	}
	if r.Detail == "" {
		t.Errorf("detail should be set")
	}
}

// ── checkForwardUpstreams ────────────────────────────────────────────────────

func TestCheckForwardUpstreams_NoneIsOK(t *testing.T) {
	r := checkForwardUpstreams(nil, false)
	if r.Status != checkOK {
		t.Errorf("expected OK when no upstreams, got %d", r.Status)
	}
}

func TestCheckForwardUpstreams_OfflineSkips(t *testing.T) {
	r := checkForwardUpstreams([]string{"http://does-not-resolve.invalid"}, true)
	if r.Status != checkWarn {
		t.Errorf("expected warn in offline mode, got %d", r.Status)
	}
	if !strings.Contains(r.Detail, "--offline") {
		t.Errorf("detail should mention --offline, got %q", r.Detail)
	}
}

func TestCheckForwardUpstreams_ReachableIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()
	r := checkForwardUpstreams([]string{srv.URL}, false)
	if r.Status != checkOK {
		t.Errorf("expected OK for live upstream, got %d (hint=%q)", r.Status, r.Hint)
	}
}

func TestCheckForwardUpstreams_UnreachableFails(t *testing.T) {
	// 127.0.0.1:1 — nothing listens, connection refused.
	r := checkForwardUpstreams([]string{"http://127.0.0.1:1"}, false)
	if r.Status != checkFail {
		t.Errorf("expected fail for unreachable upstream, got %d", r.Status)
	}
}

func TestCheckForwardUpstreams_PartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()
	r := checkForwardUpstreams([]string{srv.URL, "http://127.0.0.1:1"}, false)
	if r.Status != checkFail {
		t.Fatalf("expected fail for partial reachability, got %d", r.Status)
	}
	if !strings.Contains(r.Detail, "1/2 reachable") {
		t.Errorf("detail should report 1/2, got %q", r.Detail)
	}
}

// ── checkDuckDBVersion ───────────────────────────────────────────────────────

func TestCheckDuckDBVersion_OK(t *testing.T) {
	r := checkDuckDBVersion(":memory:")
	if r.Status != checkOK {
		t.Errorf("expected OK, got %d (hint=%q)", r.Status, r.Hint)
	}
	if !strings.Contains(r.Detail, "loaded") {
		t.Errorf("detail should mention loaded, got %q", r.Detail)
	}
}

// ── doctorModel View ─────────────────────────────────────────────────────────

func TestDoctorModel_ViewShowsAllStatesAndSummary(t *testing.T) {
	m := newDoctorModel([]checkSpec{
		{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"},
	})
	m.results = []checkResult{
		{Name: "a", Status: checkOK, Detail: "x"},
		{Name: "b", Status: checkWarn, Detail: "x", Hint: "be careful"},
		{Name: "c", Status: checkFail, Detail: "x", Hint: "fix me"},
		{Name: "d", Status: checkFail, Detail: "x"},
	}
	m.done = len(m.results)
	out := m.View()
	if !strings.Contains(out, "✓") || !strings.Contains(out, "⚠") || !strings.Contains(out, "✗") {
		t.Errorf("view should contain all three glyphs, got:\n%s", out)
	}
	if !strings.Contains(out, "2 failure") || !strings.Contains(out, "1 warning") {
		t.Errorf("summary line missing, got:\n%s", out)
	}
	if !strings.Contains(out, "fix me") {
		t.Errorf("hint not rendered, got:\n%s", out)
	}
}

func TestDoctorModel_ViewAllGreen(t *testing.T) {
	m := newDoctorModel([]checkSpec{{Name: "a"}})
	m.results = []checkResult{{Name: "a", Status: checkOK, Detail: "x"}}
	m.done = 1
	if !strings.Contains(m.View(), "all checks passed") {
		t.Errorf("missing happy-path summary:\n%s", m.View())
	}
}

// ── runDoctor (end-to-end on this process) ──────────────────────────────────

func TestRunDoctor_SmokeOnTempDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	free := func() int {
		ln, _ := net.Listen("tcp", ":0")
		p := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		return p
	}

	results := runDoctor(doctorContext{
		ConfigPath:   cfg,
		DBPath:       filepath.Join(dir, "spaniel.duckdb"),
		UIPort:       free(),
		OTLPGRPCPort: free(),
		OTLPHTTPPort: free(),
		Forward:      nil,
		Offline:      true,
	})

	for _, r := range results {
		if r.Status == checkFail {
			t.Errorf("unexpected FAIL in runDoctor: %s — %s\n  hint: %s",
				r.Name, r.Detail, r.Hint)
		}
	}
}

// ── humanBytes ──────────────────────────────────────────────────────────────

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1_572_864, "1.5 MB"},
		{2_147_483_648, "2.0 GB"},
	}
	for _, c := range cases {
		got := humanBytes(c.in)
		if got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
