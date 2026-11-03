package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zfogg/spaniel/internal/watch/client"
)

func TestResolveTraceCanonicalExactPrefixAndSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any = []any{}
		if r.URL.Query().Get("sessionId") != "pinned" {
			t.Error("lost requested scope", r.URL)
		}
		switch r.URL.Path {
		case "/api/traces/abcdef":
			data = []any{map[string]any{"trace_id": "abcdef", "span_id": "root", "session_id": "pinned"}}
		case "/api/traces":
			data = []any{map[string]any{"trace_id": "abcdef", "session_id": "pinned"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	api, _ := client.New(srv.URL, client.Options{})
	for _, id := range []string{"abcdef", "abc"} {
		trace, session, err := resolveTrace(context.Background(), api, "pinned", id)
		if err != nil || trace != "abcdef" || session != "pinned" {
			t.Fatal(trace, session, err)
		}
	}
	if _, _, err := resolveTrace(context.Background(), api, "pinned", "missing"); err == nil {
		t.Fatal("missing trace accepted")
	}
}
func TestTraceCommandKeepsTTYAndSessionContract(t *testing.T) {
	cmd := traceSubcommand()
	if cmd.Flags().Lookup("session") == nil || !strings.Contains(cmd.Short, "live") {
		t.Fatal("missing shared live/session command contract")
	}
}

func TestHumanNs(t *testing.T) {
	cases := map[int64]string{
		500:           "500ns",
		1_500:         "1µs",
		1_500_000:     "1ms",
		1_500_000_000: "1.50s",
	}
	for in, want := range cases {
		if got := humanNs(in); got != want {
			t.Errorf("humanNs(%d) = %q, want %q", in, got, want)
		}
	}
}
