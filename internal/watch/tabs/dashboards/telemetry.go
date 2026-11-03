package dashboards

import (
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/protocol"
	"github.com/zfogg/spaniel/internal/watch/tabs/logs"
	"github.com/zfogg/spaniel/internal/watch/tabs/traces"
)

type telemetryRow struct {
	key, kind, session string
	span               apigen.SpanRow
	trace              apigen.TraceRow
	log                apigen.Log
	fields             []string
	raw                map[string]any
	err                error
}

func telemetryKind(kind string) bool {
	return kind == "trace_list" || kind == "span_list" || kind == "log_list"
}

func telemetryRows(kind string, rows []map[string]any, session string) []telemetryRow {
	var out []telemetryRow
	for _, source := range rows {
		row := make(map[string]any, len(source))
		for key, value := range source {
			row[key] = value
		}
		aliases := map[string][]string{"service_name": {"service"}, "name": {"operation"}, "body": {"message", "name"}, "duration_ns": {"duration"}, "severity": {"level"}}
		for name, alternatives := range aliases {
			if row[name] == nil {
				row[name] = column(source, alternatives...)
			}
		}
		if kind == "log_list" && row["timestamp_ns"] == nil {
			if stamp, ok := timestamp(source); ok {
				row["timestamp_ns"] = stamp.UnixNano()
			}
		}
		if value, ok := row["attributes"]; ok && value != nil {
			if _, text := value.(string); !text {
				encoded, _ := json.Marshal(value)
				row["attributes"] = string(encoded)
			}
		}
		entry := telemetryRow{kind: kind, session: session, raw: source}
		for _, field := range []string{"timestamp_ns", "received_at", "service_name", "body", "severity", "trace_id", "span_id", "session_id", "attributes"} {
			if row[field] != nil {
				entry.fields = append(entry.fields, field)
			}
		}
		data, err := json.Marshal(row)
		entry.err = err
		if err == nil {
			switch kind {
			case "span_list":
				entry.err = json.Unmarshal(data, &entry.span)
				if row["session_id"] == nil {
					entry.span.SessionId = session
				}
				entry.key = entry.span.TraceId + "/" + entry.span.SpanId
			case "trace_list":
				entry.err = json.Unmarshal(data, &entry.trace)
				if row["session_id"] == nil {
					entry.trace.SessionId = session
				}
				if row["span_count"] == nil {
					entry.trace.SpanCount = -1
				}
				entry.key = entry.trace.TraceId
			case "log_list":
				entry.err = json.Unmarshal(data, &entry.log)
				if row["session_id"] == nil {
					entry.log.SessionId = session
				}
				entry.key = string(data)
			}
		}
		if entry.key == "" || entry.key == "/" {
			entry.key = string(data)
		}
		out = append(out, entry)
	}
	return out
}

func (m *model) telemetry() ([]telemetryRow, int) {
	if m.rowSelections == nil {
		m.rowSelections = map[string]string{}
	}
	d := m.dashboard()
	if m.detailID == "" || m.inspect || m.editing || d == nil || len(d.Panels) == 0 {
		return nil, 0
	}
	p := d.Panels[m.panelIndex()]
	state := m.stateFor(p.Id)
	if !telemetryKind(string(p.DisplayType)) || state == nil || !state.loaded {
		return nil, 0
	}
	rows := telemetryRows(string(p.DisplayType), state.result.Rows, m.session)
	for i, row := range rows {
		if row.key == m.rowSelections[p.Id] {
			return rows, i
		}
	}
	if len(rows) > 0 {
		m.rowSelections[p.Id] = rows[0].key
	}
	return rows, 0
}

func (r telemetryRow) action() string {
	if r.err != nil {
		return ""
	}
	switch r.kind {
	case "trace_list":
		if r.trace.TraceId != "" && r.trace.SessionId == r.session {
			return "open trace"
		}
	case "span_list":
		if r.span.SpanId != "" && r.span.SessionId == r.session {
			return "inspect span"
		}
	case "log_list":
		if r.log.TimestampNs > 0 && r.log.SessionId == r.session {
			return "full log"
		}
	}
	return ""
}

func (m *model) openTelemetry(row telemetryRow) tea.Cmd {
	back := protocol.ReturnContext{Tab: protocol.Dashboards, Selection: m.detailID}
	switch row.action() {
	case "open trace":
		return func() tea.Msg { return protocol.OpenTrace{TraceID: row.trace.TraceId, Return: back} }
	case "inspect span":
		return func() tea.Msg {
			return protocol.OpenSpan{TraceID: row.span.TraceId, SpanID: row.span.SpanId, Return: back}
		}
	case "full log":
		return func() tea.Msg {
			return protocol.ShowDetail{Return: back, New: func(env protocol.Env) tea.Model { return logs.NewQueryDetail(env, row.log, row.session, row.fields) }}
		}
	}
	return nil
}

func (m *model) telemetryView(top, body []uiLine, rows []telemetryRow, selected int) string {
	header := traces.RenderListHeader(m.width)
	if len(rows) > 0 && rows[0].kind == "log_list" {
		header = logs.RenderHeader(m.width)
	}
	body = append(body, presented(header, lipgloss.NewStyle()))
	selectedStart := 0
	for i, row := range rows {
		if i == selected {
			selectedStart = len(wrapUI(body, m.width))
		}
		var line string
		if row.err != nil {
			line = fmt.Sprintf("Row %d · typed columns unavailable: %s", i+1, row.err)
			if i == selected {
				line = "› " + line
			}
			body = append(body, ui(line, warningStyle))
			for _, item := range renderTable(apigen.QueryPreview{Rows: []map[string]any{row.raw}}, false, m.width) {
				body = append(body, presented(item, lipgloss.NewStyle()))
			}
			continue
		}
		switch row.kind {
		case "trace_list":
			line = traces.RenderTraceRow(row.trace, i == selected, m.width)
		case "span_list":
			line = traces.RenderSpanRow(row.span, i == selected, m.width)
		case "log_list":
			line = logs.RenderRow(row.log, i == selected, m.width, m.env.Now())
		}
		body = append(body, presented(line, lipgloss.NewStyle()))
		if i == selected && row.action() == "" {
			body = append(body, ui("Read-only row · correlation ID/time unavailable in the selected session.", secondaryStyle))
		}
	}
	if len(rows) == 0 {
		body = append(body, ui("No telemetry matches this query.", secondaryStyle))
	}
	available := max(1, m.height-len(wrapUI(top, m.width)))
	m.scroll = max(0, selectedStart-available/2)
	return m.compose(top, body, 0)
}

func (m *model) telemetryKey(key string) (bool, tea.Cmd) {
	rows, selected := m.telemetry()
	if len(rows) == 0 {
		return false, nil
	}
	index := selected
	switch key {
	case "enter":
		return true, m.openTelemetry(rows[selected])
	case "up", "k":
		index--
	case "down", "j":
		index++
	case "home", "g":
		index = 0
	case "end", "G":
		index = len(rows) - 1
	case "pgup":
		index -= max(1, m.height-8)
	case "pgdown":
		index += max(1, m.height-8)
	default:
		return false, nil
	}
	index = max(0, min(len(rows)-1, index))
	m.rowSelections[m.panelID] = rows[index].key
	return true, nil
}
