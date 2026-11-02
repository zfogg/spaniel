package shell

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	keys "github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func (m *recorder) ShortHelp() []keys.Binding {
	if m.capture {
		return []keys.Binding{keys.NewBinding(keys.WithKeys("enter"), keys.WithHelp("Enter", "apply input")), keys.NewBinding(keys.WithKeys("esc"), keys.WithHelp("Esc", "cancel input"))}
	}
	return []keys.Binding{keys.NewBinding(keys.WithKeys("enter"), keys.WithHelp("Enter", "inspect "+m.owner))}
}

func TestFocusedFooterAndRetainedInput(t *testing.T) {
	m, models, details := fixture(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for i, id := range protocol.Order {
		if i > 0 {
			m.Update(key("tab"))
		}
		footer := ansi.Strip(strings.Join(m.footer(), "\n"))
		if !strings.Contains(footer, "inspect "+string(id)) || !strings.Contains(footer, "Tab/Shift-Tab tabs") || strings.Contains(footer, "Esc/Backspace back") {
			t.Fatalf("%s footer: %s", id, footer)
		}
	}
	m.Update(protocol.OpenSpan{TraceID: "trace", SpanID: "span"})
	if !strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "inspect detail:1") {
		t.Fatal("detail has tab help")
	}
	details.models[0].capture = true
	m.Update(key("q"))
	footer := ansi.Strip(strings.Join(m.footer(), "\n"))
	if !strings.Contains(footer, "apply input") || strings.Contains(footer, "q quit") || strings.Contains(footer, "tabs") || !strings.Contains(footer, "Ctrl+C quit") {
		t.Fatal(footer)
	}
	m.Update(key("esc"))
	if len(m.stack) != 1 {
		t.Fatal("input escape popped detail")
	}
	details.models[0].capture = false
	m.Update(key("tab"))
	if !strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "inspect traces") {
		t.Fatal("hidden detail help leaked")
	}
	m.Update(key("shift+tab"))
	if !strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "inspect detail:1") {
		t.Fatal("retained detail help lost")
	}
	m.Update(key("esc"))
	if !strings.Contains(ansi.Strip(strings.Join(m.footer(), "\n")), "inspect alerts") || len(models[6].keys) != 0 {
		t.Fatal("Back did not restore list context")
	}
}

func TestOneRowFooterPopupGeometryAndInput(t *testing.T) {
	m, models, _ := fixture(t)
	m.status.Err = errors.New(strings.Repeat("long failure\n\x1b[31m", 100))
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 30}, {Width: 30, Height: 12}, {Width: 8, Height: 6}, {Width: 1, Height: 1}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m.Update(size)
			for _, popup := range []bool{false, true} {
				m.helpOpen = popup
				for range 4 {
					lines := strings.Split(m.View(), "\n")
					if len(lines) > size.Height || len(m.footer()) != 1 {
						t.Fatal("one-row geometry overflow", len(lines), len(m.footer()))
					}
					for _, line := range lines {
						if ansi.StringWidth(line) > size.Width {
							t.Fatalf("width overflow %d: %q", size.Width, line)
						}
					}
					if m.contentHeight() != max(0, size.Height-2) {
						t.Fatal("wrong reserved geometry")
					}
					m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
				}
			}
			m.helpOpen = false
		})
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	if !strings.Contains(ansi.Strip(m.footer()[0]), "? help") {
		t.Fatal("overflow popup is undiscoverable", m.footer())
	}
	before := len(models[0].keys)
	m.Update(key("?"))
	if !m.helpOpen || !strings.Contains(ansi.Strip(m.View()), "Keyboard help") {
		t.Fatal("popup did not open")
	}
	m.Update(key("j"))
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.helpOffset == 0 || len(models[0].keys) != before {
		t.Fatal("popup scroll reached feature or did not move")
	}
	m.Update(key("esc"))
	if m.helpOpen {
		t.Fatal("popup did not close")
	}
	models[0].capture = true
	m.Update(key("?"))
	if m.helpOpen || models[0].keys[len(models[0].keys)-1] != "?" {
		t.Fatal("literal question mark was captured by help")
	}
	if !strings.Contains(ansi.Strip(m.footer()[0]), "F2 help") {
		t.Fatal("input help alternative missing", m.footer())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyF2})
	if !m.helpOpen {
		t.Fatal("input help did not open")
	}
	if strings.Contains(ansi.Strip(strings.Join(m.helpRows(80), "\n")), "q quit") {
		t.Fatal("inapplicable globals in input popup")
	}
	m.Update(key("esc"))
	if !models[0].capture || m.helpOpen {
		t.Fatal("closing help cancelled the field")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyF1})
	if m.helpOpen {
		t.Fatal("obsolete F1 paging remains")
	}
}
