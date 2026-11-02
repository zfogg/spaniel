package alerts

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/shell"
)

type terminalOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (o *terminalOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buffer.Write(p)
}
func (o *terminalOutput) text() string { o.mu.Lock(); defer o.mu.Unlock(); return o.buffer.String() }

type otherTab struct{}

func (otherTab) Init() tea.Cmd                       { return nil }
func (otherTab) Update(tea.Msg) (tea.Model, tea.Cmd) { return otherTab{}, nil }
func (otherTab) View() string                        { return "Other tab for background lifecycle verification" }

// Run the actual shell and alert models with the real shared socket against
// representative HTTP data. ANSI captures come from Bubble Tea's renderer.
func TestAlertsTerminalRuntime(t *testing.T) {
	if os.Getenv("SPANIEL_ALERTS_CAPTURE_DIR") != "" {
		profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
		t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	}
	f, env := newFixture(t)
	updates, err := env.API.Stream(env.Context)
	if err != nil {
		t.Fatal(err)
	}
	var tabs []protocol.Tab
	for _, id := range protocol.Order {
		factory := protocol.Factory(func(protocol.Env) tea.Model { return otherTab{} })
		if id == protocol.Alerts {
			factory = New
		}
		tabs = append(tabs, protocol.Tab{ID: id, Title: string(id), New: factory})
	}
	app := shell.New(env, tabs, nil, updates, true)
	var output terminalOutput
	p := tea.NewProgram(app, tea.WithInput(nil), tea.WithOutput(&output), tea.WithContext(env.Context), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	p.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	wait := func(text string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for !strings.Contains(output.text(), text) {
			if time.Now().After(deadline) {
				t.Fatalf("runtime did not render %q:\n%s", text, output.text())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	key := func(r rune) { p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }
	capture := func(name string) {
		t.Helper()
		dir := os.Getenv("SPANIEL_ALERTS_CAPTURE_DIR")
		if dir == "" {
			return
		}
		time.Sleep(150 * time.Millisecond)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(output.text()), 0644); err != nil {
			t.Fatal(err)
		}
	}
	wait("Checkout latency")
	capture("watch-alerts")
	p.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("Condition / threshold")
	capture("watch-alert-definition")
	key('i')
	wait("842.7")
	capture("watch-alert-instances")
	p.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("State: firing")
	capture("watch-alert-instance-detail")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	key('h')
	wait("Latency exceeded 500ms")
	capture("watch-alert-history")
	p.Send(tea.KeyMsg{Type: tea.KeyEnter})
	wait("Event: e3")
	capture("watch-alert-event-detail")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	key('i')
	key('l')
	wait(spanID)
	capture("watch-alert-links")
	// Keep a retained detail and its results live while a different tab is open.
	f.mu.Lock()
	initialDetailReads := 0
	for _, request := range f.requests {
		if request.Path == "/api/alerts/latency" {
			initialDetailReads++
		}
	}
	f.mu.Unlock()
	p.Send(tea.KeyMsg{Type: tea.KeyTab})
	wait("Other tab for background")
	f.mu.Lock()
	f.rules[0].Name = "Recovered checkout"
	f.rules[0].Instances[0].State = "resolved"
	f.mu.Unlock()
	f.hub.Broadcast(liveevent.NewResourcesChangedEvent([]liveevent.Resource{"alerts"}, "evaluation", ""))
	// Wait for both owners' HTTP snapshots to finish without reading UI state
	// from outside its event loop.
	deadline := time.Now().Add(8 * time.Second)
	for {
		f.mu.Lock()
		n := 0
		for _, r := range f.requests {
			if r.Path == "/api/alerts/latency" {
				n++
			}
		}
		f.mu.Unlock()
		if n > initialDetailReads {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hidden detail not refreshed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	p.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	key('i')
	wait("Recovered checkout")
	capture("watch-alert-resolved")
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	wait("Recovered checkout")
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime failed to quit")
	}
}

// Exercise nested inspection through the real shell, whose global Escape and
// Backspace handling must restore one pane rather than leave the rule browser.
func TestShellInspectionBackPreservesRuleContext(t *testing.T) {
	f, env := newFixture(t)
	f.mu.Lock()
	f.events = nil
	for i := 0; i < 32; i++ {
		f.events = append(f.events, apigen.AlertEvent{Id: fmt.Sprintf("event-%02d", i), RuleId: "latency", Kind: "firing", State: "firing", Detail: "history detail", GroupKey: "checkout"})
	}
	f.mu.Unlock()
	app := shell.New(env, []protocol.Tab{{ID: protocol.Alerts, Title: "alerts", New: New}}, nil, nil, true)
	var dispatch func(tea.Msg)
	dispatch = func(msg tea.Msg) {
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, cmd := range batch {
				if cmd != nil {
					dispatch(cmd())
				}
			}
			return
		}
		if msg == nil {
			return
		}
		_, cmd := app.Update(msg)
		if cmd != nil {
			dispatch(cmd())
		}
	}
	connected := client.Update{Status: &client.Status{Connected: true}}
	dispatch(tea.WindowSizeMsg{Width: 120, Height: 30})
	dispatch(connected)
	key := func(key string) {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		switch key {
		case "enter":
			msg.Type = tea.KeyEnter
		case "esc":
			msg.Type = tea.KeyEsc
		case "backspace":
			msg.Type = tea.KeyBackspace
		case "down":
			msg.Type = tea.KeyDown
		}
		dispatch(msg)
		dispatch(connected)
	}
	check := func(parts ...string) {
		t.Helper()
		view := ansi.Strip(app.View())
		for _, part := range parts {
			if !strings.Contains(view, part) {
				t.Fatalf("missing %q:\n%s", part, view)
			}
		}
	}
	key("enter")
	key("i")
	key("down")
	key("/")
	key("payments")
	key("enter")
	check("Viewing: instances", "search payments", "> resolved / 121.4  payments")
	key("enter")
	check("Viewing: instance", "Group: payments")
	key("esc")
	check("Viewing: instances", "search payments", "> resolved / 121.4  payments")
	key("h")
	key("s")
	key("s")
	key("]")
	key("down")
	check("Viewing: history", "API page 2/2", "state firing")
	key("enter")
	check("Viewing: event", "Event: event-26")
	key("backspace")
	check("Viewing: history", "API page 2/2", "state firing")
	key("enter")
	check("Event: event-26")
	key("esc")
	key("esc")
	check("Alert rules", "> firing / critical  Checkout latency")
}
