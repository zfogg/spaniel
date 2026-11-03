package lints

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func testEnv(owner string) protocol.Env {
	ctx := context.Background()
	r := client.NewRefreshes(ctx, nil)
	r.SetSession("demo")
	return protocol.Env{Context: ctx, Refresh: r, Owner: owner}
}
func fixtures() ([]apigen.LintWarning, []apigen.TraceIssue, []apigen.SpanRow) {
	return []apigen.LintWarning{
		{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span", RuleId: "http.request.method", Severity: "error", Message: "Use http.request.method instead of deprecated http.method", CreatedAt: 1793631000000000001},
		{SessionId: "demo", RuleId: "service.name", Severity: "warning", Message: "Missing service.name resource attribute", CreatedAt: 1793631000000000000},
		{SessionId: "demo", TraceId: "checkout-trace", SpanId: "db-span", RuleId: "n_plus_one", Severity: "warning", Message: "N+1 query: 24 repeated executions", CreatedAt: 1793630999000000000},
	}, []apigen.TraceIssue{
		{Id: "detector-1", SessionId: "demo", TraceId: "checkout-trace", ExampleSpanId: "db-span", ParentSpanId: "http-span", Kind: "n_plus_one", Fingerprint: "SELECT * FROM products WHERE id = ?", Count: 24, WastedNs: 92000000, CreatedAt: 1793630999000000000},
		{Id: "detector-2", SessionId: "demo", TraceId: "checkout-trace", ExampleSpanId: "http-span", Kind: "slow_span", Fingerprint: "GET /checkout", Count: 3, WastedNs: 420000000, CreatedAt: 1793630998000000000},
	}, []apigen.SpanRow{{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span", ServiceName: "checkout-api"}, {SessionId: "demo", TraceId: "checkout-trace", SpanId: "db-span", ServiceName: "catalog-db"}}
}
func deliver(m *model, name string, data any) {
	s := m.states[name]
	m.Update(protocol.Result{Key: client.Key{Owner: m.env.Owner, Name: name}, Session: m.session, Sequence: s.sequence + 1, Data: data})
}
func seed(m *model) {
	l, i, s := fixtures()
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: l})
	deliver(m, "issues", client.Envelope[[]apigen.TraceIssue]{Data: i})
	deliver(m, "services", client.Envelope[[]apigen.SpanRow]{Data: s})
	m.Update(protocol.Connection{Connected: true})
}
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}
func TestSemanticDetectorFiltersSelectionAndBounds(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	if len(m.rows) != 4 {
		t.Fatalf("detector summary duplicated: %+v", m.rows)
	}
	for filter, count := range map[string]int{"severity:error": 1, "rule:n_plus_one service:catalog-db SELECT": 1, "source:detector": 2, "service:checkout-api": 2, "missing": 1, "service:unknown": 0} {
		m.filter = filter
		m.refilter()
		if len(m.visible) != count {
			t.Errorf("%s: got %d want %d", filter, len(m.visible), count)
		}
	}
	m.filter = ""
	m.refilter()
	m.Update(key("j"))
	selected := m.selected
	l, _, _ := fixtures()
	l = append(l, apigen.LintWarning{SessionId: "demo", RuleId: "new", CreatedAt: 1793632000000000000})
	m.Update(protocol.Focus{Active: false})
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: l})
	if m.selected != selected {
		t.Fatal("hidden update moved selection")
	}
	m.Update(protocol.ReturnContext{Tab: protocol.Lints, Selection: selected})
	if m.visible[m.cursor].id != selected {
		t.Fatal("Back lost selection")
	}
	for j := 0; j < rowLimit+50; j++ {
		l = append(l, apigen.LintWarning{SessionId: "demo", RuleId: fmt.Sprint(j), CreatedAt: int64(j)})
	}
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: l})
	if len(m.rows) > rowLimit || len(m.lints) > rowLimit {
		t.Fatal("unbounded findings")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.cursor != len(m.visible)-1 {
		t.Fatal("paging did not reach last finding")
	}
}
func openDetail(t *testing.T, m *model) *model {
	t.Helper()
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("no detail action")
	}
	show, ok := cmd().(protocol.ShowDetail)
	if !ok || show.Return.Selection != m.selected {
		t.Fatal("missing return context")
	}
	e := m.env
	e.Owner = "detail:1"
	d := show.New(e).(*model)
	d.Update(protocol.Connection{Connected: true})
	d.Update(protocol.SessionChanged{ID: "demo"})
	return d
}
func target(d *model) {
	deliver(d, "target", client.Envelope[[]apigen.Span]{Data: []apigen.Span{{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span", ServiceName: "checkout-api"}, {SessionId: "demo", TraceId: "checkout-trace", SpanId: "db-span", ServiceName: "catalog-db"}}})
}
func TestDetailUpdatesSharedNavigationExpiryAndSession(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	d := openDetail(t, m)
	seed(d)
	target(d)
	d.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	for i := 0; i < 100; i++ {
		d.Update(key("j"))
	}
	last := d.scroll
	d.Update(key("k"))
	if d.scroll != last-1 {
		t.Fatal("scrolling past the end trapped the detail at the bottom")
	}
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if d.scroll != 0 {
		t.Fatal("resize did not clamp detail scroll")
	}
	if !strings.Contains(d.View(), "Use http.request.method") || !strings.Contains(d.View(), "checkout-api") {
		t.Fatal(d.View())
	}
	if cmd := d.navigate("s"); cmd == nil {
		t.Fatal("span link missing")
	} else if msg, ok := cmd().(protocol.OpenSpan); !ok || msg.SpanID != "http-span" || msg.Return.Selection != d.detail.id {
		t.Fatalf("bad span navigation %#v", cmd())
	}
	if cmd := d.navigate("t"); cmd == nil {
		t.Fatal("trace link missing")
	} else if msg, ok := cmd().(protocol.OpenTrace); !ok || msg.TraceID != "checkout-trace" {
		t.Fatal("bad trace navigation")
	}
	l, _, _ := fixtures()
	l[0].Message = "Updated semantic finding while hidden"
	d.Update(protocol.Focus{Active: false})
	deliver(d, "lint", client.Envelope[[]apigen.LintWarning]{Data: l})
	if d.detail.message != l[0].Message {
		t.Fatal("hidden detail stale")
	}
	d.Update(protocol.Connection{Connected: false})
	if d.navigate("s") != nil || !strings.Contains(d.View(), "STALE") {
		t.Fatal("disconnected state")
	}
	d.Update(protocol.Connection{Connected: true, Resync: true})
	if d.navigate("s") != nil {
		t.Fatal("navigation before authoritative resync")
	}
	target(d)
	deliver(d, "lint", client.Envelope[[]apigen.LintWarning]{Data: l})
	deliver(d, "target", client.Envelope[[]apigen.Span]{})
	if d.navigate("s") != nil || !strings.Contains(d.View(), "deleted or expired") {
		t.Fatal(d.View())
	}
	deliver(d, "lint", client.Envelope[[]apigen.LintWarning]{})
	if d.found || !strings.Contains(d.View(), "Finding removed") {
		t.Fatal("retention did not expire finding")
	}
	d.Update(protocol.SessionChanged{ID: "next"})
	if !d.wrongSession || d.navigate("t") != nil {
		t.Fatal("old session detail navigable")
	}
	m.Update(protocol.SessionChanged{ID: "next"})
	if len(m.rows) != 0 || m.selected != "" {
		t.Fatal("session data leaked")
	}
}
func TestDetectorDetailCountAndAbsentCorrelation(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	m.filter = "rule:n_plus_one"
	m.refilter()
	d := openDetail(t, m)
	seed(d)
	target(d)
	_, issues, _ := fixtures()
	issues[0].Count = 39
	issues[0].ExampleSpanId = "http-span"
	deliver(d, "issues", client.Envelope[[]apigen.TraceIssue]{Data: issues})
	if !d.found || d.detail.count != 39 || d.detail.span != "http-span" {
		t.Fatalf("updated detector lost identity: %+v", d.detail)
	}
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(d.View(), "Fingerprint: SELECT") || !strings.Contains(d.View(), "92ms") {
		t.Fatal(d.View())
	}
	m.filter = "rule:service.name"
	m.refilter()
	absent := openDetail(t, m)
	seed(absent)
	if absent.navigate("t") != nil || !strings.Contains(absent.View(), "No trace/span correlation") {
		t.Fatal(absent.View())
	}
}
func TestLoadingErrorsStaleGuardsInputAndResize(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	if !strings.Contains(m.View(), "Loading lint") {
		t.Fatal("loading missing")
	}
	seed(m)
	old := m.selected
	m.Update(protocol.Result{Key: client.Key{Owner: "other", Name: "lint"}, Session: "demo", Sequence: 99, Data: client.Envelope[[]apigen.LintWarning]{}})
	m.Update(protocol.Result{Key: client.Key{Owner: "lints", Name: "lint"}, Session: "old", Sequence: 99, Data: client.Envelope[[]apigen.LintWarning]{}})
	m.Update(protocol.Result{Key: client.Key{Owner: "lints", Name: "lint"}, Session: "demo", Sequence: 1, Data: client.Envelope[[]apigen.LintWarning]{}})
	if m.selected != old || len(m.rows) != 4 {
		t.Fatal("stale/foreign result applied")
	}
	m.Update(protocol.Result{Key: client.Key{Owner: "lints", Name: "lint"}, Session: "demo", Sequence: 2, Err: errors.New("HTTP 500 unavailable")})
	if len(m.rows) != 4 || !strings.Contains(m.View(), "previous data stale") {
		t.Fatal("failed refresh erased data")
	}
	_, cmd := m.Update(key("/"))
	if !m.CapturesKeys() || cmd == nil {
		t.Fatal("filter focus")
	}
	if _, ok := cmd().(protocol.Owned); !ok {
		t.Fatal("widget command not owned")
	}
	m.Update(key("rule:http"))
	m.Update(key("enter"))
	if m.CapturesKeys() || len(m.visible) != 1 {
		t.Fatal("filter entry")
	}
	m.Update(key("/"))
	m.Update(key("q"))
	m.Update(key("esc"))
	if m.filter != "rule:http" {
		t.Fatal("cancel changed filter")
	}
	d := openDetail(t, m)
	seed(d)
	target(d)
	for _, width := range []int{1, 12, 34, 80} {
		for _, height := range []int{1, 6, 20} {
			for _, pane := range []*model{m, d} {
				pane.Update(tea.WindowSizeMsg{Width: width, Height: height})
				view := pane.View()
				if len(strings.Split(view, "\n")) > height {
					t.Fatal("height overflow")
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("width overflow %d: %q", width, line)
					}
				}
			}
		}
	}
	if strings.Contains(clean("hello\x1b[2J\nworld"), "\x1b") {
		t.Fatal("unsafe control bytes")
	}
	empty := New(testEnv("empty")).(*model)
	deliver(empty, "lint", client.Envelope[[]apigen.LintWarning]{})
	deliver(empty, "issues", client.Envelope[[]apigen.TraceIssue]{})
	if !strings.Contains(empty.View(), "No stored findings") {
		t.Fatal(empty.View())
	}
}
func TestCompleteInvalidationContract(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	m.Update(protocol.Live{Event: liveevent.Message{Type: "self_span"}})
	if m.states["services"].pending {
		t.Fatal("self telemetry refetch loop")
	}
	m.Update(protocol.Live{Event: liveevent.Message{Type: "resources_changed", Decoded: &liveevent.ResourcesChangedPayload{Resources: []liveevent.Resource{"lint", "issues"}}}})
	if !m.states["lint"].pending || !m.states["issues"].pending {
		t.Fatal("durable lint invalidation missing")
	}
}

func TestSpanOnlyCorrelationAndSummaryUpgrade(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	w := apigen.LintWarning{SessionId: "demo", SpanId: "http-span", RuleId: "http.method", Severity: "warning", Message: "Legacy span-only finding"}
	deliver(m, "lint", client.Envelope[[]apigen.LintWarning]{Data: []apigen.LintWarning{w}})
	deliver(m, "issues", client.Envelope[[]apigen.TraceIssue]{})
	d := openDetail(t, m)
	deliver(d, "lint", client.Envelope[[]apigen.LintWarning]{Data: []apigen.LintWarning{w}})
	deliver(d, "target", client.Envelope[apigen.Span]{Data: apigen.Span{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span", ServiceName: "checkout-api"}})
	if d.navigate("s") == nil || d.detail.service != "checkout-api" {
		t.Fatal("span-only finding did not resolve")
	}
	d.Update(protocol.Live{Event: liveevent.Message{Type: "issue"}})
	if d.states["target"].pending || d.navigate("s") == nil {
		t.Fatal("trace-only invalidation stranded a span-only target subscription")
	}
	deliver(d, "lint", client.Envelope[[]apigen.LintWarning]{Data: []apigen.LintWarning{w}})
	if d.navigate("t") == nil {
		t.Fatal("refresh lost resolved trace")
	}
	deliver(d, "target", client.Envelope[apigen.Span]{})
	if d.navigate("s") != nil {
		t.Fatal("expired span-only correlation navigable")
	}

	l, i, _ := fixtures()
	summary := New(testEnv("summary")).(*model)
	deliver(summary, "lint", client.Envelope[[]apigen.LintWarning]{Data: l[2:]})
	upgraded := openDetail(t, summary)
	deliver(upgraded, "issues", client.Envelope[[]apigen.TraceIssue]{Data: i})
	if !upgraded.found || upgraded.detail.source != "detector" || upgraded.detail.count != 24 {
		t.Fatal("early summary detail did not upgrade")
	}
}

func TestDetectorAssociationChangeRepairsCompletedTarget(t *testing.T) {
	m := New(testEnv("lints")).(*model)
	seed(m)
	m.filter = "rule:n_plus_one"
	m.refilter()
	d := openDetail(t, m)
	seed(d)
	target(d)
	_, issues, _ := fixtures()
	issues[0].ExampleSpanId = "new-example-span"
	d.Update(protocol.Focus{Active: false})
	deliver(d, "issues", client.Envelope[[]apigen.TraceIssue]{Data: issues})
	if !d.env.Refresh.Pending(client.Key{Owner: d.env.Owner, Name: "target"}) || d.navigate("s") != nil {
		t.Fatal("changed detector association did not schedule guarded validation")
	}
	deliver(d, "target", client.Envelope[[]apigen.Span]{Data: []apigen.Span{{SessionId: "demo", TraceId: "checkout-trace", SpanId: "http-span"}, {SessionId: "demo", TraceId: "checkout-trace", SpanId: "new-example-span", ServiceName: "catalog-db"}}})
	cmd := d.navigate("s")
	if cmd == nil {
		t.Fatal("new example span was not validated")
	}
	if msg, ok := cmd().(protocol.OpenSpan); !ok || msg.SpanID != "new-example-span" {
		t.Fatal("navigation used the previous example span")
	}
}
