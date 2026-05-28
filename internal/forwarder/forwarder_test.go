package forwarder

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond up to ~500ms before failing the test.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestNewEmpty(t *testing.T) {
	f := New(nil, 1.0)
	if f == nil {
		t.Fatal("New returned nil")
	}
	if s := f.Status(); len(s) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(s))
	}
}

func TestForwardSingleUpstream(t *testing.T) {
	var (
		gotPath        string
		gotContentType string
		gotBody        []byte
		calls          atomic.Int32
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	f.Forward("/v1/traces", "application/json", []byte(`{"hello":"world"}`))

	waitFor(t, func() bool { return calls.Load() == 1 })

	if gotPath != "/v1/traces" {
		t.Errorf("expected path /v1/traces, got %q", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected content-type application/json, got %q", gotContentType)
	}
	if string(gotBody) != `{"hello":"world"}` {
		t.Errorf("unexpected body: %q", gotBody)
	}

	st := f.Status()
	if len(st) != 1 {
		t.Fatalf("expected 1 status, got %d", len(st))
	}
	if st[0].Sent != 1 {
		t.Errorf("expected sent=1, got %d", st[0].Sent)
	}
	if st[0].Errors != 0 {
		t.Errorf("expected errors=0, got %d", st[0].Errors)
	}
	if st[0].LastErr != "" {
		t.Errorf("expected no last_error, got %q", st[0].LastErr)
	}
}

func TestForwardMultipleUpstreams(t *testing.T) {
	var calls [2]atomic.Int32

	make := func(i int) *httptest.Server {
		idx := i
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[idx].Add(1)
			w.WriteHeader(http.StatusOK)
		}))
	}
	srv0 := make(0)
	defer srv0.Close()
	srv1 := make(1)
	defer srv1.Close()

	f := New([]string{srv0.URL, srv1.URL}, 1.0)
	f.Forward("/v1/logs", "application/x-protobuf", []byte("binary"))

	waitFor(t, func() bool { return calls[0].Load() == 1 && calls[1].Load() == 1 })

	st := f.Status()
	if len(st) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(st))
	}
	for i, s := range st {
		if s.Sent != 1 {
			t.Errorf("upstream %d: expected sent=1, got %d", i, s.Sent)
		}
		if s.Errors != 0 {
			t.Errorf("upstream %d: expected errors=0, got %d", i, s.Errors)
		}
	}
}

func TestForwardUpstreamError_4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	f.Forward("/v1/traces", "application/json", []byte("{}"))

	waitFor(t, func() bool { return f.Status()[0].Errors == 1 })

	st := f.Status()
	if st[0].Sent != 0 {
		t.Errorf("expected sent=0, got %d", st[0].Sent)
	}
	if st[0].Errors != 1 {
		t.Errorf("expected errors=1, got %d", st[0].Errors)
	}
	if st[0].LastErr == "" {
		t.Error("expected last_error to be set")
	}
}

func TestForwardUpstreamUnreachable(t *testing.T) {
	// Port 1 is reserved/unrouteable — connection refused quickly.
	f := New([]string{"http://127.0.0.1:1"}, 1.0)
	f.Forward("/v1/traces", "application/json", []byte("{}"))

	waitFor(t, func() bool { return f.Status()[0].Errors == 1 })

	st := f.Status()
	if st[0].Sent != 0 {
		t.Errorf("expected sent=0, got %d", st[0].Sent)
	}
	if st[0].Errors != 1 {
		t.Errorf("expected errors=1, got %d", st[0].Errors)
	}
	if st[0].LastErr == "" {
		t.Error("expected last_error to be set")
	}
}

func TestForwardCountersAccumulate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	const n = 5
	for range n {
		f.Forward("/v1/metrics", "application/json", []byte("{}"))
	}

	waitFor(t, func() bool { return f.Status()[0].Sent == n })

	st := f.Status()
	if st[0].Sent != n {
		t.Errorf("expected sent=%d, got %d", n, st[0].Sent)
	}
}

func TestForwardSamplesAtRate(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const sample = 0.1
	const n = 1000

	f := New([]string{srv.URL}, sample)
	// Deterministic pseudo-random: 10/100 == sample exactly.
	var i int
	f.rand = func() float64 {
		i++
		return float64(i%100) / 100.0
	}

	for range n {
		f.Forward("/v1/traces", "application/json", []byte("{}"))
	}

	// All decisions are synchronous; only successful sends spawn goroutines.
	waitFor(t, func() bool { return f.Status()[0].Sent+f.Status()[0].Skipped == n })

	st := f.Status()[0]
	expected := int64(n * sample)
	// Deterministic generator → exact counts.
	if st.Sent != expected {
		t.Errorf("sent = %d, want %d", st.Sent, expected)
	}
	if st.Skipped != int64(n)-expected {
		t.Errorf("skipped = %d, want %d", st.Skipped, int64(n)-expected)
	}
	if int64(received.Load()) != st.Sent {
		t.Errorf("upstream got %d requests, status says sent=%d", received.Load(), st.Sent)
	}
}

func TestForwardSampleZeroSkipsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream should not be called when sample=0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 0)
	for range 50 {
		f.Forward("/v1/traces", "application/json", []byte("{}"))
	}

	st := f.Status()[0]
	if st.Sent != 0 || st.Skipped != 50 {
		t.Errorf("expected sent=0 skipped=50, got sent=%d skipped=%d", st.Sent, st.Skipped)
	}
}

func TestStatusURLsPreserved(t *testing.T) {
	urls := []string{"http://a:4318", "http://b:4318", "http://c:4318"}
	f := New(urls, 1.0)
	st := f.Status()
	if len(st) != len(urls) {
		t.Fatalf("expected %d statuses, got %d", len(urls), len(st))
	}
	for i, s := range st {
		if s.URL != urls[i] {
			t.Errorf("status[%d].URL = %q, want %q", i, s.URL, urls[i])
		}
	}
}
