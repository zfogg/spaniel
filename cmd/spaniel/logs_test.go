package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newWSPusher returns a server that accepts a WS connection and immediately
// pushes the given frames. Shared with watch_test.go — the streaming
// subcommands all consume the same WS shape.
func newWSPusher(t *testing.T, frames []map[string]any) (*httptest.Server, *sync.WaitGroup) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck
		for _, f := range frames {
			data, _ := json.Marshal(f)
			if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
				return
			}
		}
		// Give the client a beat to consume before we close.
		time.Sleep(50 * time.Millisecond)
	}))
	return srv, &wg
}

func TestLogFilter_MatchService(t *testing.T) {
	f := logFilter{Service: "wanted"}
	if !f.match(&logEntry{ServiceName: "wanted"}) {
		t.Errorf("matching service should pass")
	}
	if f.match(&logEntry{ServiceName: "other"}) {
		t.Errorf("non-matching service should fail")
	}
}

func TestLogFilter_MatchSeverity(t *testing.T) {
	f := logFilter{MinSeverity: severityFromName("warn")}
	if f.match(&logEntry{Severity: 9}) {
		t.Errorf("info should be filtered at warn minimum")
	}
	if !f.match(&logEntry{Severity: 13}) {
		t.Errorf("warn should pass at warn minimum")
	}
}

func TestLogFilter_MatchTracePrefix(t *testing.T) {
	f := logFilter{TraceID: "abcd"}
	if !f.match(&logEntry{TraceID: "abcd1234"}) {
		t.Errorf("prefix should match longer trace id")
	}
	if f.match(&logEntry{TraceID: "ffff"}) {
		t.Errorf("non-prefix should not match")
	}
}

func TestStyleLogLine_ContainsBodyAndService(t *testing.T) {
	l := &logEntry{Severity: 17, Body: "something failed", ServiceName: "api", TraceID: "0123456789abcdef"}
	out := styleLogLine(l)
	for _, want := range []string{"ERROR", "something failed", "api", "01234567"} {
		if !strings.Contains(out, want) {
			t.Errorf("styleLogLine missing %q: %q", want, out)
		}
	}
}

func TestSeverityFromName(t *testing.T) {
	cases := map[string]int{
		"trace": 1, "DEBUG": 5, "info": 9, "warn": 13, "warning": 13,
		"error": 17, "err": 17, "fatal": 21, "unknown": 0, "": 0,
	}
	for in, want := range cases {
		if got := severityFromName(in); got != want {
			t.Errorf("severityFromName(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestHttpToWS(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8080":  "ws://localhost:8080",
		"https://example.com":    "wss://example.com",
		"http://localhost:8080/": "ws://localhost:8080",
	}
	for in, want := range cases {
		got, err := httpToWS(in)
		if err != nil {
			t.Errorf("httpToWS(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("httpToWS(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := httpToWS("ftp://nope"); err == nil {
		t.Error("expected error for non-http(s) scheme")
	}
}
