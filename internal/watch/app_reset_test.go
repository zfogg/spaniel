package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/viper"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/ws"
)

type observeTransport func(*http.Request) (*http.Response, error)

func (f observeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the production shell subscription against durable storage and the
// real reset/activation APIs. No synthetic active_session frame repairs reset.
func TestTerminalRecoversActiveSessionAfterAPIReset(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old, err := store.CreateSession("old", false)
	if err != nil {
		t.Fatal(err)
	}
	store.SetActiveSession(old.ID, old.Label)
	now := time.Now().UnixNano()
	if err := store.InsertSpan(&storage.Span{TraceID: "old-trace", SpanID: "old-span", Name: "old", ServiceName: "api", SessionID: old.ID, SessionLabel: old.Label, StartNs: now, EndNs: now + 1, Attributes: "{}", Resource: "{}"}); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	store.SetChangeNotifier(func(names []string, reason string) {
		resources := make([]liveevent.Resource, len(names))
		for i, name := range names {
			resources[i] = liveevent.Resource(name)
		}
		hub.Broadcast(liveevent.NewResourcesChangedEvent(resources, reason, ""))
	})
	router := api.NewRouterFull(store, hub, nil, nil, &api.SettingsService{Viper: viper.New()}, nil, nil, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(router)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	type observation struct {
		path, session string
		data          json.RawMessage
	}
	observed := make(chan observation, 128)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	apiClient, err := client.New(srv.URL, client.Options{HTTP: &http.Client{Transport: observeTransport(func(r *http.Request) (*http.Response, error) {
		resp, err := transport.RoundTrip(r)
		if err != nil || (r.URL.Path != "/api/traces" && r.URL.Path != "/api/sessions/active") {
			return resp, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		var envelope struct{ Data json.RawMessage }
		if err == nil && resp.StatusCode == http.StatusOK && json.Unmarshal(body, &envelope) == nil {
			select {
			case observed <- observation{r.URL.Path, r.URL.Query().Get("sessionId"), envelope.Data}:
			case <-r.Context().Done():
			}
		}
		return resp, err
	})}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model, err := New(ctx, apiClient, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithContext(ctx))
	done := make(chan struct{})
	go func() { defer close(done); p.Run() }()
	defer func() {
		cancel()
		p.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("terminal did not stop")
		}
	}()
	wait := func(description string, match func(observation) bool) {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case got := <-observed:
				if match(got) {
					return
				}
			case <-timer.C:
				t.Fatalf("terminal did not recover %s", description)
			}
		}
	}
	traceRows := func(session string, count int) func(observation) bool {
		return func(got observation) bool {
			var rows []json.RawMessage
			return got.path == "/api/traces" && got.session == session && json.Unmarshal(got.data, &rows) == nil && len(rows) == count
		}
	}
	wait("initial populated active scope", traceRows(old.ID, 1))
	request := func(method, path, body string) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s: %d %s (%v)", method, path, resp.StatusCode, data, err)
		}
		return data
	}
	request(http.MethodDelete, "/api/settings/data", "")
	wait("cleared active metadata", func(got observation) bool {
		var active struct{ ID, Label string }
		return got.path == "/api/sessions/active" && json.Unmarshal(got.data, &active) == nil && active.ID == "" && active.Label == ""
	})
	wait("empty unscoped trace cache", traceRows("", 0))
	var created struct{ Data storage.Session }
	if err := json.Unmarshal(request(http.MethodPost, "/api/sessions", `{"label":"empty new session"}`), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("create empty session: %v", err)
	}
	request(http.MethodPost, "/api/sessions/"+created.Data.ID+"/activate", "{}")
	wait("new empty active scope", traceRows(created.Data.ID, 0))
}
