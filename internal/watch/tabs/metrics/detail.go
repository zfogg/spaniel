package metrics

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/liveevent"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/charts"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

var windows = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}

type seriesSnapshot struct {
	Data     seriesData
	From, To time.Time
}
type detail struct {
	env                                        protocol.Env
	metric                                     apigen.MetricCatalogEntry
	data                                       seriesData
	key                                        client.Key
	revision                                   int
	session, selected, operation, query, draft string
	editing, loaded, overlay                   bool
	err                                        error
	connection                                 protocol.Connection
	width, height, window, offset              int
	now                                        time.Time
}

func newDetail(env protocol.Env, row apigen.MetricCatalogEntry) *detail {
	m := &detail{env: env, metric: row, session: env.Refresh.Session(), width: 100, height: 28, window: 2, now: time.Now(), operation: defaultOperation(row)}
	m.register()
	return m
}
func (m *detail) Init() tea.Cmd      { return nil }
func (m *detail) CapturesKeys() bool { return m.editing }
func (m *detail) register() {
	if m.key.Name != "" {
		m.env.Refresh.Remove(m.key)
	}
	m.revision++
	m.key = client.Key{Owner: m.env.Owner, Name: fmt.Sprintf("series:%d", m.revision)}
	// Only immutable query state is captured. The scheduler supplies session and
	// bounds HTTP concurrency, reconnect repair and idle-window refreshes.
	name, service, window := m.metric.Name, m.metric.ServiceName, windows[m.window]
	m.env.Refresh.Register(client.Spec{Key: m.key, Resources: []liveevent.Resource{"metric-series"}, Interval: 5 * time.Second,
		Fetch: func(ctx context.Context, api *client.Client, session string) (any, error) {
			to := time.Now()
			from := to.Add(-window)
			query := url.Values{"name": {name}, "service": {service}, "operation": {"raw"}, "from": {strconv.FormatInt(from.UnixNano(), 10)}, "to": {strconv.FormatInt(to.UnixNano(), 10)}}
			response, err := api.MetricSeries(ctx, client.Scope(query, session))
			if err != nil {
				return nil, err
			}
			data, err := normalize(response.Data)
			if err != nil {
				return nil, err
			}
			data.bound(from.UnixNano(), to.UnixNano())
			return seriesSnapshot{Data: data, From: from, To: to}, nil
		},
	})
}
func (m *detail) candidates() []labeledSeries {
	query := m.query
	if m.editing {
		query = m.draft
	}
	rows := make([]labeledSeries, 0, len(m.data.Series))
	for _, s := range m.data.Series {
		matches := true
		for _, term := range strings.Fields(strings.ToLower(query)) {
			key, value, hasKey := strings.Cut(term, "=")
			if hasKey {
				found := false
				for k, v := range s.Attributes {
					if strings.ToLower(k) == key && strings.Contains(strings.ToLower(fmt.Sprint(v)), value) {
						found = true
						break
					}
				}
				if !found {
					matches = false
				}
			} else if !strings.Contains(strings.ToLower(jsonText(s.Attributes)), term) {
				matches = false
			}
		}
		if matches {
			rows = append(rows, s)
		}
	}
	return rows
}
func (m *detail) reconcile() {
	rows := m.candidates()
	// Keep the chosen label identity while a replacement window is loading.
	if len(rows) == 0 && !m.loaded {
		return
	}
	for _, s := range rows {
		if s.Key == m.selected {
			return
		}
	}
	m.selected = ""
	if len(rows) > 0 {
		m.selected = rows[0].Key
	}
	m.offset = 0
}
func (m *detail) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case protocol.Connection:
		m.connection = msg
	case protocol.SessionChanged:
		if msg.ID != m.session {
			m.session = msg.ID
			m.data = seriesData{}
			m.selected = ""
			m.loaded = false
			m.err = nil
			m.offset = 0
		}
	case protocol.Result:
		if msg.Key != m.key || msg.Session != m.session {
			break
		}
		m.err = msg.Err
		if msg.Err == nil {
			if result, ok := msg.Data.(seriesSnapshot); ok {
				m.data = result.Data
				m.loaded = true
				m.data.bound(m.now.Add(-windows[m.window]).UnixNano(), max(m.now.UnixNano(), result.To.UnixNano()))
				m.reconcile()
			}
		}
	case protocol.Tick:
		if msg.Time.Unix() > m.now.Unix() {
			m.now = msg.Time
			m.data.bound(m.now.Add(-windows[m.window]).UnixNano(), m.now.UnixNano())
			m.reconcile()
		}
	case protocol.Live:
		if row := metricRow(msg.Event, m.session); row != nil && row.Name == m.metric.Name && row.ServiceName == m.metric.ServiceName {
			if row.TimestampNs < m.now.Add(-windows[m.window]).UnixNano() || row.TimestampNs > m.now.Add(time.Second).UnixNano() {
				break
			}
			// The event contains the complete admitted series identity; never merge
			// across arbitrary labels, services or sessions, including self telemetry.
			key := row.SeriesKey
			if key == "" {
				key = row.Name + "\x00" + row.ServiceName + "\x00" + row.SeriesAttributes
			}
			index := -1
			for i, s := range m.data.Series {
				if s.Key == key {
					index = i
					break
				}
			}
			if index < 0 {
				if len(m.data.Series) >= seriesLimit {
					m.data.Capped = true
					break
				}
				attrs := map[string]any{}
				if jsonDecode(row.SeriesAttributes, &attrs) != nil {
					break
				}
				m.data.Series = append(m.data.Series, labeledSeries{Key: key, Attributes: attrs})
				index = len(m.data.Series) - 1
			}
			m.data.Name = row.Name
			m.data.Service = row.ServiceName
			m.data.Type = row.Type
			m.data.Unit = row.Unit
			m.data.Description = row.Description
			m.data.Temporality = row.AggregationTemporality
			m.data.Monotonic = row.IsMonotonic
			m.data.Series[index].Points = append(m.data.Series[index].Points, rowSample(row))
			m.data.bound(m.now.Add(-windows[m.window]).UnixNano(), m.now.Add(time.Second).UnixNano())
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
			m.offset = 0
			m.reconcile()
			return m, nil
		}
		rows := m.candidates()
		index := 0
		for i, s := range rows {
			if s.Key == m.selected {
				index = i
				break
			}
		}
		switch msg.String() {
		case "/":
			m.editing = true
			m.draft = m.query
		case "left", "[":
			if len(rows) > 0 {
				m.selected = rows[max(0, index-1)].Key
				m.offset = 0
			}
		case "right", "]":
			if len(rows) > 0 {
				m.selected = rows[min(len(rows)-1, index+1)].Key
				m.offset = 0
			}
		case "up", "k":
			m.offset = max(0, m.offset-1)
		case "down", "j":
			m.offset++
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height-6))
		case "pgdown":
			m.offset += max(1, m.height-6)
		case "home":
			m.offset = 0
		case "end":
			m.offset = 1 << 30
		case "a":
			m.overlay = !m.overlay
			m.offset = 0
		case "o":
			ops := operations(m.kind())
			for i, op := range ops {
				if op == m.operation {
					m.operation = ops[(i+1)%len(ops)]
					break
				}
			}
			m.offset = 0
		case "w":
			m.window = (m.window + 1) % len(windows)
			m.data = seriesData{}
			m.loaded = false
			m.offset = 0
			m.err = nil
			m.register()
		}
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		header, body := m.render()
		m.offset = min(m.offset, max(0, len(body)-max(1, m.height-len(header)-1)))
	}
	return m, nil
}
func (m *detail) kind() string {
	if m.data.Type != "" {
		return m.data.Type
	}
	return m.metric.Type
}
func (m *detail) render() ([]string, []string) {
	unit := m.data.Unit
	if unit == "" {
		unit = m.metric.Unit
	}
	if unit == "" {
		unit = "1"
	}
	if m.operation == "rate" {
		unit += "/s"
	}
	temporality := m.data.Temporality
	if temporality == "" && m.metric.AggregationTemporality != nil {
		temporality = *m.metric.AggregationTemporality
	}
	header := []string{tui.Title.Render(safe(m.metric.Name)) + " · " + safe(m.metric.ServiceName), fmt.Sprintf("%s · %s · %s · view %s · window %s", safe(m.kind()), safe(temporality), safe(unit), m.operation, windows[m.window]), tui.Muted.Render("←→ series · / labels · o operation · w window · a overlay · ↑↓/PgDn scroll")}
	if m.data.Monotonic != nil {
		header[1] += fmt.Sprintf(" · monotonic %t", *m.data.Monotonic)
	}
	if s := state(m.loaded, m.err, m.connection); s != "" {
		header = append(header, s)
	}
	query := m.query
	if m.editing {
		query = m.draft + "▏"
	}
	if query != "" {
		header = append(header, "Label filter: "+safe(query))
	}
	body := []string{safe(m.metric.Description), "Session: " + safe(m.session) + " · UTC · " + timestamp(m.now.Add(-windows[m.window]).UnixNano()) + " → " + timestamp(m.now.UnixNano())}
	if m.data.Capped {
		body = append(body, "Bounded view: first 256 series, latest 2048 samples per series; no aggregation.")
	}
	rows := m.candidates()
	index := -1
	for i, s := range rows {
		if s.Key == m.selected {
			index = i
			break
		}
	}
	if index < 0 {
		if m.loaded {
			body = append(body, "No samples in this time window/session or label filter. Waiting for telemetry.")
		}
	} else {
		current := rows[index]
		body = append(body, fmt.Sprintf("Series %d/%d (%d in window) · %d samples", index+1, len(rows), len(m.data.Series), len(current.Points)), "Labels: "+jsonText(current.Attributes))
		if m.kind() == "exponential_histogram" || m.kind() == "summary" {
			body = append(body, "Distribution table: scalar/percentile chart unavailable for this type; raw observations below.")
		} else if m.kind() == "histogram" && m.operation == "raw" {
			body = append(body, "Raw histogram: inspect count/sum/buckets below; o selects interval mean or estimated percentile.")
		} else if m.kind() != "gauge" && m.kind() != "sum" && m.kind() != "counter" && m.kind() != "histogram" {
			body = append(body, "Unsupported metric presentation: raw metadata below; no scalar chart.")
		} else {
			sets := []charts.Series{}
			candidates := []labeledSeries{current}
			if m.overlay {
				candidates = rows[:min(8, len(rows))]
				body = append(body, "Overlay: first 8 matching series; independent labels, no cross-series aggregation.")
			}
			for i, s := range candidates {
				set := charts.Series{Name: strconv.Itoa(i + 1)}
				// Separate datasets at unavailable observations so the line never bridges
				// a no-value flag or an interval whose rate is undefined.
				part := 0
				for j, p := range s.Points {
					v, ok := plottedValue(s.Points, j, m.data, m.operation)
					if !ok {
						if len(set.Points) > 0 {
							sets = append(sets, set)
							part++
							set = charts.Series{Name: fmt.Sprintf("%d:%d", i+1, part)}
						}
						continue
					}
					set.Points = append(set.Points, charts.Point{Time: time.Unix(0, p.Timestamp), Value: v})
				}
				if len(set.Points) > 0 {
					sets = append(sets, set)
				}
				if m.overlay {
					body = append(body, fmt.Sprintf("%d: %s", i+1, jsonText(s.Attributes)))
				}
			}
			if m.width >= 40 && m.height >= 15 {
				body = append(body, strings.Split(charts.RenderTimeSeries(m.width-2, min(10, max(4, m.height/3)), m.now.Add(-windows[m.window]), m.now, sets), "\n")...)
			} else {
				body = append(body, "Chart hidden at this terminal size; sample table remains available.")
			}
			if m.operation == "rate" || m.operation == "delta" {
				body = append(body, "Derived from raw exports per series; missing baseline/interval = unavailable. Resets start a new interval.")
			}
			if m.kind() == "histogram" {
				body = append(body, "Histogram intervals: cumulative snapshots differenced; first/reset uses snapshot. Percentiles estimate bucket bounds.")
			}
		}
		body = append(body, tui.Title.Render("Authoritative samples · newest first · "+safe(unit)))
		for i := len(current.Points) - 1; i >= 0; i-- {
			p := current.Points[i]
			v, ok := plottedValue(current.Points, i, m.data, m.operation)
			display := "unavailable"
			if ok {
				display = number(v)
			}
			if m.kind() == "gauge" || m.kind() == "sum" || m.kind() == "counter" || m.kind() == "histogram" {
				body = append(body, fmt.Sprintf("%s = %s %s", m.operation, display, safe(unit)))
			}
			body = append(body, sampleDescription(p, m.kind())...)
			if m.operation == "rate" {
				body = append(body, "Raw fields use the original unit: "+safe(m.metric.Unit))
			}
			body = append(body, "")
		}
		keys := make([]string, 0, len(m.data.Dimensions))
		for k := range m.data.Dimensions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			body = append(body, "Dimension "+safe(k)+": "+jsonText(m.data.Dimensions[k]))
		}
	}
	wrapped := []string{}
	for _, line := range body {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, m.width, true), "\n")...)
	}
	return header, wrapped
}

func (m *detail) View() string {
	header, wrapped := m.render()
	available := max(1, m.height-len(header)-1)
	offset := min(m.offset, max(0, len(wrapped)-available))
	header = append(header, wrapped[offset:min(len(wrapped), offset+available)]...)
	header = append(header, tui.Muted.Render(fmt.Sprintf("Scroll %d/%d · Esc returns to catalog", offset+1, max(1, len(wrapped)-available+1))))
	return fit(header, m.width, m.height)
}
