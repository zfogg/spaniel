package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCLIExitCodesAndPrivateErrors(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	capture := filepath.Join(dir, "capture.json")
	if err := os.WriteFile(spec, []byte(`{"version":1,"counts":[{"name":"one","min":1,"max":1}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture, []byte(`[{"trace_id":"00000000000000000000000000000001","span_id":"0000000000000001","name":"private-value"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run([]string{"--spec", spec, "--input", capture}, &out, &errs); code != 0 {
		t.Fatalf("passing exit %d: %s", code, errs.String())
	}
	if strings.Contains(out.String(), "private-value") {
		t.Fatal("success report leaked name")
	}
	os.WriteFile(capture, []byte(`[]`), 0600)
	out.Reset()
	errs.Reset()
	if code := run([]string{"--spec", spec, "--input", capture}, &out, &errs); code != 1 || !strings.Contains(out.String(), "count_out_of_bounds") {
		t.Fatalf("failure exit %d: %s", code, out.String())
	}
	os.WriteFile(capture, []byte(`private-value`), 0600)
	out.Reset()
	errs.Reset()
	if code := run([]string{"--spec", spec, "--input", capture}, &out, &errs); code != 2 || strings.Contains(errs.String(), "private-value") {
		t.Fatalf("input exit %d: %s", code, errs.String())
	}
	if code := run([]string{}, &out, &errs); code != 2 {
		t.Fatal("missing flags accepted")
	}
}

func loopbackServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestCLILivePollingTokenAndNoDuplicateAccumulation(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	os.WriteFile(spec, []byte(`{"version":1,"no_duplicate_span_ids":true,"counts":[{"name":"one","min":1,"max":1}]}`), 0600)
	t.Setenv("SPANIEL_API_TOKEN", "private-token")
	var requests atomic.Int32
	server := loopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing bearer token")
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		fmt.Fprint(w, `{"data":[{"trace_id":"00000000000000000000000000000001","span_id":"0000000000000001","attributes":"{}","events":[],"links":[]}]}`)
	}))
	var out, errs bytes.Buffer
	code := run([]string{"--spec", spec, "--spaniel-url", server.URL, "--trace-id", "00000000000000000000000000000001", "--wait", "500ms", "--interval", "50ms"}, &out, &errs)
	if code != 0 || requests.Load() != 2 {
		t.Fatalf("live polling: code=%d requests=%d stderr=%s", code, requests.Load(), errs.String())
	}
	if strings.Contains(out.String()+errs.String(), "private-token") {
		t.Fatal("token leaked into report")
	}
}

func TestCLIRejectsCrossOriginRedirectWithoutForwardingToken(t *testing.T) {
	spec := filepath.Join(t.TempDir(), "spec.json")
	os.WriteFile(spec, []byte(`{"version":1,"no_duplicate_span_ids":true}`), 0600)
	t.Setenv("SPANIEL_API_TOKEN", "private-token")
	var forwarded atomic.Bool
	other := loopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Store(true) }))
	server := loopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	var out, errs bytes.Buffer
	code := run([]string{"--spec", spec, "--spaniel-url", server.URL, "--trace-id", "00000000000000000000000000000001"}, &out, &errs)
	if code != 2 || forwarded.Load() {
		t.Fatal("cross-origin collector redirect followed")
	}
	if strings.Contains(errs.String(), "private-token") {
		t.Fatal("token leaked in error")
	}
}
