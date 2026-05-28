package forwarder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLastErrMessagePreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	f.Forward("/v1/traces", "application/json", []byte("{}"))

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if f.Status()[0].Errors > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := f.Status()
	if st[0].Errors == 0 {
		t.Fatal("expected errors>0 for 503 response")
	}
	if !strings.Contains(st[0].LastErr, "503") {
		t.Errorf("expected LastErr to contain '503', got %q", st[0].LastErr)
	}
}

func TestForward5xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	f.Forward("/v1/traces", "application/json", []byte("{}"))

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if f.Status()[0].Errors > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := f.Status()
	if st[0].Errors == 0 {
		t.Error("expected errors to increment for 500 response")
	}
	if st[0].Sent != 0 {
		t.Errorf("expected sent=0 for error response, got %d", st[0].Sent)
	}
}

func TestConcurrentForward(t *testing.T) {
	var (
		mu    sync.Mutex
		count int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			f.Forward("/v1/traces", "application/json", []byte("{}"))
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := count
		mu.Unlock()
		if c == n {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	finalCount := count
	mu.Unlock()

	if finalCount != n {
		t.Errorf("expected %d requests to arrive at upstream, got %d", n, finalCount)
	}

	st := f.Status()
	if st[0].Sent != n {
		t.Errorf("expected sent=%d, got %d", n, st[0].Sent)
	}
	if st[0].Errors != 0 {
		t.Errorf("expected errors=0, got %d", st[0].Errors)
	}
}

func TestForwardEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := New([]string{srv.URL}, 1.0)
	f.Forward("/v1/logs", "application/json", []byte(""))

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if f.Status()[0].Sent == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := f.Status()
	if st[0].Sent != 1 {
		t.Errorf("expected sent=1 for empty body, got %d", st[0].Sent)
	}
}
