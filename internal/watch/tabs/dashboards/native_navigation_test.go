package dashboards

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func TestLinkedPreviewRowsUseSharedNavigationAndRetainSelectionDuringRepair(t *testing.T) {
	d := definition(t, `{"id":"native","name":"Native results","panels":[{"id":"traces","title":"Traces","position":0,"display_type":"trace_list","query_sql":"SELECT traces","settings_json":"{}"},{"id":"spans","title":"Spans","position":1,"display_type":"span_list","query_sql":"SELECT spans","settings_json":"{}"},{"id":"logs","title":"Logs","position":2,"display_type":"log_list","query_sql":"SELECT logs","settings_json":"{}"}]}`)
	var count atomic.Int64
	count.Store(2)
	env, refresh := envFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/api/dashboards":
			data = []apigen.Dashboard{d}
		case "/api/sessions/active":
			data = apigen.ActiveSession{Id: "other-active"}
		case "/api/dashboards/native/query-preview":
			if r.Method != "POST" {
				t.Error("preview method", r.Method)
			}
			var body apigen.PreviewDashboardQueryJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.SessionId == nil || *body.SessionId != "pinned" {
				t.Error("lost selected preview session", body.SessionId)
			}
			switch body.QuerySql {
			case "SELECT traces":
				data = preview(t, `{"rows":[{"trace_id":"trace","name":"request","service_name":"api","duration_ns":240000000,"span_count":`+strconv.FormatInt(count.Load(), 10)+`}]}`)
			case "SELECT spans":
				data = preview(t, `{"rows":[{"trace_id":"trace","span_id":"one","name":"first","service_name":"api","duration_ns":240000000,"status_code":2},{"trace_id":"trace","span_id":"two","name":"second","service_name":"api","duration_ns":120000000,"status_code":2}]}`)
			case "SELECT logs":
				data = preview(t, `{"rows":[{"timestamp_ns":1793631000000000001,"severity":17,"body":"query projected log","service_name":"api","trace_id":"trace","span_id":"two"}]}`)
			}
		default:
			t.Error("unexpected or mutation request", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	refresh.SetSession("pinned")
	m := newModel(env, "native")
	m.Update(protocol.SessionChanged{ID: "pinned"})
	m.Update(protocol.Connection{Connected: true})
	now := time.Now()
	for i := 0; i < 6; i++ {
		deliver(m, refresh, now)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	trace, ok := cmd().(protocol.OpenTrace)
	if !ok || trace.TraceID != "trace" || trace.Return.Tab != protocol.Dashboards {
		t.Fatal(trace)
	}
	m.panelID = "spans"
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	span, ok := cmd().(protocol.OpenSpan)
	if !ok || span.SpanID != "two" || span.Return.Selection != "native" {
		t.Fatal(span)
	}
	selected := m.rowSelections["spans"]
	m.Update(protocol.Focus{Active: false})
	count.Store(3)
	refresh.Resync()
	m.Update(protocol.Connection{Connected: true, Resync: true})
	for i := 0; i < 6; i++ {
		deliver(m, refresh, now.Add(time.Second))
	}
	m.Update(span.Return)
	if m.panelID != "spans" || m.rowSelections["spans"] != selected || !strings.Contains(m.View(), "second") {
		t.Fatal("repair lost panel/row context", m.View())
	}
	m.panelID = "traces"
	if !strings.Contains(m.View(), "3 spans") {
		t.Fatal("hidden reconnect result did not repair", m.View())
	}
	m.panelID = "logs"
	rows, _ := m.telemetry()
	if rows[0].log.TimestampNs != 1793631000000000001 {
		t.Fatal("projection rounded timestamp", rows)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	log := cmd().(protocol.ShowDetail)
	childEnv := env
	childEnv.Owner = "detail:log"
	if !strings.Contains(log.New(childEnv).View(), "Full log") || log.Return.Tab != protocol.Dashboards {
		t.Fatal("log panel used another inspector")
	}
	for _, source := range []map[string]any{{"name": "unlinked", "span_id": "", "trace_id": ""}, {"name": "foreign", "span_id": "two", "trace_id": "trace", "session_id": "other"}} {
		if action := telemetryRows("span_list", []map[string]any{source}, "pinned")[0].action(); action != "" {
			t.Fatal("fabricated/cross-session link", action)
		}
	}
}
