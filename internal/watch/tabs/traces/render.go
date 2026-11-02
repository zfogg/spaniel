package traces

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef6b73"))

type generation struct{ epoch, sequence uint64 }
type viewState struct {
	session                  string
	connection               protocol.Connection
	loading, loaded, pending bool
	err                      string
	last                     map[string]generation
}

func newState(env protocol.Env) viewState {
	return viewState{session: env.Refresh.Session(), loading: true, last: map[string]generation{}}
}
func (s *viewState) reset(session string) {
	s.session = session
	s.loading = true
	s.loaded = false
	s.pending = false
	s.err = ""
	s.last = map[string]generation{}
}
func (s viewState) matches(session string) bool { return s.session == "" || session == s.session }

// The shell rejects superseded requests before dispatch. These checks also make
// the renderer safe to embed: only its owner's ordered, scoped result is used.
func (s *viewState) accept(r protocol.Result, owner, name string) bool {
	if r.Key.Owner != owner || r.Key.Name != name || r.Session != s.session {
		return false
	}
	old, exists := s.last[name]
	if exists && (r.Epoch < old.epoch || r.Sequence <= old.sequence) {
		return false
	}
	s.last[name] = generation{r.Epoch, r.Sequence}
	s.loading = false
	s.pending = r.Pending
	if r.Err != nil {
		s.err = r.Err.Error()
	} else {
		s.loaded = true
		s.err = ""
	}
	return true
}
func (s viewState) banner() string {
	var parts []string
	if s.loading {
		parts = append(parts, "Loading stored data…")
	}
	if s.err != "" {
		parts = append(parts, "STALE · Error: "+clean(s.err))
	}
	if !s.connection.Connected {
		parts = append(parts, "STALE · disconnected; reconnecting")
	} else if s.pending {
		parts = append(parts, "Syncing durable changes…")
	} else if !s.loading && s.err == "" {
		parts = append(parts, "LIVE")
	}
	return tui.Muted.Render(strings.Join(parts, " · "))
}

func eventSpan(e liveevent.Message) (apigen.Span, bool) {
	var row *liveevent.SelfSpanPayload
	switch p := e.Decoded.(type) {
	case *liveevent.SelfSpanPayload:
		row = p
	case *liveevent.SpanPayload:
		row = p.Row
	}
	if row == nil || row.SpanID == "" || row.TraceID == "" {
		return apigen.Span{}, false
	}
	return decodeRow[apigen.Span](row)
}
func eventTrace(e liveevent.Message) (apigen.TraceRow, bool) {
	var row *liveevent.SelfTracePayload
	switch p := e.Decoded.(type) {
	case *liveevent.SelfTracePayload:
		row = p
	case *liveevent.SpanPayload:
		row = p.Trace
	}
	if row == nil || row.TraceID == "" {
		return apigen.TraceRow{}, false
	}
	return decodeRow[apigen.TraceRow](row)
}
func eventLog(e liveevent.Message) (apigen.Log, bool) {
	var row *liveevent.SelfLogPayload
	switch p := e.Decoded.(type) {
	case *liveevent.SelfLogPayload:
		row = p
	case *liveevent.LogPayload:
		row = p.Row
	}
	if row == nil {
		return apigen.Log{}, false
	}
	return decodeRow[apigen.Log](row)
}
func decodeRow[T any](v any) (T, bool) {
	var out T
	data, err := json.Marshal(v)
	if err != nil {
		return out, false
	}
	err = json.Unmarshal(data, &out)
	return out, err == nil
}
func convert[T any](v any) T               { out, _ := decodeRow[T](v); return out }
func spanRow(s apigen.Span) apigen.SpanRow { return convert[apigen.SpanRow](s) }

// Telemetry is untrusted terminal text, including stored attributes and IDs.
func clean(s string) string { return terminal.SingleLine(s) }

func clip(s string, w int) string { return ansi.Truncate(s, max(0, w), "…") }
func pad(s string, w int) string {
	s = clip(s, w)
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func short(s string) string {
	r := []rune(clean(s))
	if len(r) > 16 {
		return string(r[:8]) + "…" + string(r[len(r)-5:])
	}
	return string(r)
}
func fallback(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
func duration(ns int64) string {
	if ns < 0 {
		return "invalid"
	}
	if ns == 0 {
		return "0s"
	}
	if ns < 1000 {
		return "<1µs"
	}
	if ns < int64(time.Millisecond) {
		return time.Duration(ns).Round(time.Microsecond).String()
	}
	return time.Duration(ns).Round(time.Millisecond).String()
}
func statusText(code float64) string {
	if code >= 2 {
		return "ERROR"
	}
	if code == 1 {
		return "OK"
	}
	return "UNSET"
}
func stamp(ns int64, now ...time.Time) string {
	if ns <= 0 {
		return "(missing or invalid)"
	}
	return terminal.Timestamp(ns, "2006-01-02 15:04:05.000 MST", time.UTC, terminal.BatchTime(now...))
}
func rowWindow(n, cursor, height int) (int, int) {
	height = max(1, height)
	start := max(0, min(cursor-height/2, n-height))
	return start, min(n, start+height)
}
func screen(lines []string, w, h int) string {
	if h <= 0 {
		return ""
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = clip(line, max(1, w))
	}
	return strings.Join(out, "\n")
}
