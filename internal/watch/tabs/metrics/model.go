// Package metrics implements the live metric catalog and series inspector.
package metrics

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

const catalogLimit = 4096

type catalog struct {
	env                             protocol.Env
	rows                            []apigen.MetricCatalogEntry
	selected, query, draft, session string
	editing, loaded, capped         bool
	err                             error
	connection                      protocol.Connection
	width, height                   int
}

// New is the stable shell constructor. Registration happens on the UI goroutine.
func New(env protocol.Env) tea.Model {
	m := &catalog{env: env, session: env.Refresh.Session(), width: 100, height: 28}
	env.Refresh.Register(client.Spec{
		Key: client.Key{Owner: env.Owner, Name: "catalog"}, Resources: []liveevent.Resource{"metrics"},
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			return api.Metrics(ctx, client.Scope(url.Values{}, session))
		},
	})
	return m
}
func (m *catalog) Init() tea.Cmd      { return nil }
func (m *catalog) CapturesKeys() bool { return m.editing }

// ShortHelp is consumed structurally by the integration footer. Inline legends
// remain until that shared shell change can move all feature help together.
func (m *catalog) ShortHelp() []key.Binding {
	if m.editing {
		return inputHelp("search", m.draft)
	}
	out := []key.Binding{binding("/", "search metrics", "/")}
	if len(m.filtered()) > 0 {
		out = append(out, binding("↑/↓", "select metric", "up", "down", "j", "k"), binding("PgUp/PgDn", "page metrics", "pgup", "pgdown"), binding("Enter", "inspect series", "enter"))
	}
	if m.query != "" {
		out = append(out, binding("Esc", "clear search", "esc"))
	}
	return out
}
func binding(label, action string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, action))
}
func inputHelp(subject, draft string) []key.Binding {
	out := []key.Binding{binding("Enter", "apply "+subject, "enter"), binding("Esc", "cancel "+subject, "esc")}
	if draft != "" {
		out = append(out, binding("Backspace", "erase character", "backspace"))
	}
	return out
}
func identity(row apigen.MetricCatalogEntry) string { return row.ServiceName + "\x00" + row.Name }

// Catalog snapshots may group descriptor variants separately (for example when
// an exporter changes its description). The series endpoint addresses a stream
// by service/name, so keep that same identity and combine its disjoint counts.
func catalogRows(rows []apigen.MetricCatalogEntry) []apigen.MetricCatalogEntry {
	byID := make(map[string]apigen.MetricCatalogEntry, len(rows))
	for _, row := range rows {
		id := identity(row)
		if old, ok := byID[id]; ok {
			count := old.SampleCount + row.SampleCount
			if old.LastTimestampNs > row.LastTimestampNs {
				row = old
			}
			row.SampleCount = count
		}
		byID[id] = row
	}
	out := make([]apigen.MetricCatalogEntry, 0, len(byID))
	for _, row := range byID {
		out = append(out, row)
	}
	return out
}
func (m *catalog) filtered() []apigen.MetricCatalogEntry {
	rows := make([]apigen.MetricCatalogEntry, 0, len(m.rows))
	query := m.query
	if m.editing {
		query = m.draft
	}
	for _, row := range m.rows {
		if m.env.Filters.Service != "" && row.ServiceName != m.env.Filters.Service {
			continue
		}
		match := true
		for _, term := range strings.Fields(strings.ToLower(query)) {
			field, value, qualifier := strings.Cut(term, "=")
			text := row.Name + " " + row.ServiceName + " " + row.Type + " " + row.Unit + " " + row.Description
			if qualifier {
				switch field {
				case "service":
					text = row.ServiceName
				case "type":
					text = row.Type
				case "unit":
					text = row.Unit
				default:
					value = term
				}
			} else {
				value = term
			}
			if !strings.Contains(strings.ToLower(text), value) {
				match = false
				break
			}
		}
		if match {
			rows = append(rows, row)
		}
	}
	return rows
}
func (m *catalog) reconcile() {
	rows := m.filtered()
	for _, row := range rows {
		if identity(row) == m.selected {
			return
		}
	}
	m.selected = ""
	if len(rows) > 0 {
		m.selected = identity(rows[0])
	}
}
func (m *catalog) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case protocol.Connection:
		m.connection = msg
	case protocol.SessionChanged:
		if msg.ID != m.session {
			m.session = msg.ID
			m.rows = nil
			m.selected = ""
			m.loaded = false
			m.err = nil
			m.capped = false
		}
	case protocol.Result:
		if msg.Key != (client.Key{Owner: m.env.Owner, Name: "catalog"}) || msg.Session != m.session {
			break
		}
		m.err = msg.Err
		if msg.Err == nil {
			if value, ok := msg.Data.(client.Envelope[[]apigen.MetricCatalogEntry]); ok {
				m.rows = catalogRows(value.Data)
				sort.Slice(m.rows, func(i, j int) bool { return identity(m.rows[i]) < identity(m.rows[j]) })
				m.capped = len(m.rows) > catalogLimit
				if m.capped {
					m.rows = m.rows[:catalogLimit]
				}
				m.loaded = true
				m.reconcile()
			}
		}
	case protocol.Live:
		if row := metricRow(msg.Event, m.session); row != nil {
			entry := apigen.MetricCatalogEntry{Name: row.Name, ServiceName: row.ServiceName, Type: row.Type, Unit: row.Unit, Description: row.Description, AggregationTemporality: &row.AggregationTemporality, IsMonotonic: row.IsMonotonic, LastTimestampNs: row.TimestampNs}
			found := false
			for i := range m.rows {
				if identity(m.rows[i]) == identity(entry) {
					// Replayed rows cannot tell us an exact aggregate sample count.
					if row.TimestampNs >= m.rows[i].LastTimestampNs {
						entry.SampleCount = m.rows[i].SampleCount
						m.rows[i] = entry
					}
					found = true
					break
				}
			}
			if !found && len(m.rows) < catalogLimit {
				m.rows = append(m.rows, entry)
				sort.Slice(m.rows, func(i, j int) bool { return identity(m.rows[i]) < identity(m.rows[j]) })
			}
			m.reconcile()
		}
	case protocol.ReturnContext:
		if msg.Tab == protocol.Metrics {
			m.selected = msg.Selection
			m.reconcile()
		}
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "enter":
				m.query = m.draft
				m.editing = false
			case "esc":
				m.draft = m.query
				m.editing = false
			case "backspace":
				r := []rune(m.draft)
				if len(r) > 0 {
					m.draft = string(r[:len(r)-1])
				}
			default:
				if msg.Type == tea.KeyRunes && len([]rune(m.draft)) < 256 {
					m.draft += string(msg.Runes)
				}
				if msg.Type == tea.KeySpace {
					m.draft += " "
				}
			}
			m.reconcile()
			return m, nil
		}
		rows := m.filtered()
		index := 0
		for i, row := range rows {
			if identity(row) == m.selected {
				index = i
				break
			}
		}
		switch msg.String() {
		case "/":
			m.editing = true
			m.draft = m.query
		case "esc":
			m.query = ""
			m.reconcile()
		case "up", "k":
			if len(rows) > 0 {
				m.selected = identity(rows[max(0, index-1)])
			}
		case "down", "j":
			if len(rows) > 0 {
				m.selected = identity(rows[min(len(rows)-1, index+1)])
			}
		case "pgup":
			if len(rows) > 0 {
				m.selected = identity(rows[max(0, index-max(1, m.height-10))])
			}
		case "pgdown":
			if len(rows) > 0 {
				m.selected = identity(rows[min(len(rows)-1, index+max(1, m.height-10))])
			}
		case "home":
			if len(rows) > 0 {
				m.selected = identity(rows[0])
			}
		case "end":
			if len(rows) > 0 {
				m.selected = identity(rows[len(rows)-1])
			}
		case "enter":
			if len(rows) > 0 {
				row := rows[index]
				return m, func() tea.Msg {
					return protocol.ShowDetail{New: func(env protocol.Env) tea.Model { return newDetail(env, row) }, Return: protocol.ReturnContext{Tab: protocol.Metrics, Selection: identity(row)}}
				}
			}
		}
	}
	return m, nil
}

func (m *catalog) View() string {
	query := m.query
	if m.editing {
		query = m.draft + "▏"
	}
	lines := []string{tui.Title.Render("Metric catalog"), tui.Muted.Render("/ search · service=api type=sum unit=ms · ↑↓ select · Enter inspect"), "Search: " + safe(query)}
	if m.env.Filters.Service != "" {
		lines = append(lines, "Service scope: "+safe(m.env.Filters.Service))
	}
	if s := state(m.loaded, m.err, m.connection); s != "" {
		lines = append(lines, s)
	}
	rows := m.filtered()
	lines = append(lines, fmt.Sprintf("%d matching streams · catalog counts are snapshot samples, not series", len(rows)))
	if m.capped {
		lines = append(lines, "Catalog limited to 4096 streams; narrow the server scope.")
	}
	if len(rows) == 0 && m.loaded {
		lines = append(lines, "No metrics match this search/session. Waiting for stored telemetry.")
	}
	start, index := 0, 0
	for i, row := range rows {
		if identity(row) == m.selected {
			index = i
			break
		}
	}
	capacity := max(1, (m.height-len(lines)-3)/2)
	if index >= capacity {
		start = index - capacity + 1
	}
	for _, row := range rows[start:min(len(rows), start+capacity)] {
		prefix := "  "
		if identity(row) == m.selected {
			prefix = "› "
		}
		count := number(row.SampleCount)
		if row.SampleCount == 0 {
			count = "pending"
		}
		line := fmt.Sprintf("%s%s  %s  [%s] %s · %s samples", prefix, safe(row.Name), safe(row.ServiceName), safe(row.Type), safe(row.Unit), count)
		if identity(row) == m.selected {
			line = tui.SelectedRow.Render(cut(line, m.width))
		}
		lines = append(lines, line, "  "+safe(row.Description))
	}
	if len(rows) > 0 {
		lines = append(lines, tui.Muted.Render(fmt.Sprintf("%d/%d · Enter: labeled series, dimensions, chart and raw samples", index+1, len(rows))))
	}
	return fit(lines, m.width, m.height)
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func cut(s string, width int) string { return ansi.Truncate(s, max(1, width), "…") }
func fit(lines []string, width, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = cut(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
func state(loaded bool, err error, connection protocol.Connection) string {
	if err != nil {
		return tui.Danger.Render("Error: " + safe(err.Error()) + " · retrying; retained data is stale")
	}
	if !loaded {
		return tui.Warn.Render("Loading stored metrics…")
	}
	if !connection.Connected {
		return tui.Warn.Render("Stale · disconnected; retained data may be incomplete")
	}
	return ""
}
func timestamp(ns int64) string {
	if ns == 0 {
		return "—"
	}
	return time.Unix(0, ns).UTC().Format("2006-01-02 15:04:05.000Z")
}
