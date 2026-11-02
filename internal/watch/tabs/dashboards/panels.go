package dashboards

import (
	"encoding/json"
	"fmt"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/charts"
	"github.com/zfogg/spaniel/internal/watch/terminal"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type settings struct {
	Unit       string            `json:"unit"`
	TimeWindow float64           `json:"time_window_seconds"`
	Min        any               `json:"min"`
	Max        any               `json:"max"`
	Thresholds []any             `json:"thresholds"`
	Columns    map[string]string `json:"columns"`
}

func renderPanel(kind, raw string, data apigen.QueryPreview, width, height int, live bool, now ...time.Time) []string {
	reference := terminal.BatchTime(now...)
	var config settings
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	configErr := decoder.Decode(&config)
	lines := []string{}
	if config.Unit != "" {
		lines = append(lines, "Unit: "+config.Unit)
	}
	for _, warning := range data.Warnings {
		lines = append(lines, "Warning: "+warning)
	}
	if data.Truncated != nil && *data.Truncated {
		lines = append(lines, "Truncated: first 128 query rows; refine the query/window.")
	}
	if len(data.Rows) == 0 {
		return append(lines, "No telemetry matches this query.")
	}
	switch kind {
	case "bar", "pie", "entity_list", "gauge", "heatmap":
		if configErr != nil && strings.TrimSpace(raw) != "" {
			return append(lines, "Invalid panel settings: "+configErr.Error())
		}
		switch kind {
		case "gauge":
			lines = append(lines, renderGauge(config, data.Rows, width)...)
		case "heatmap":
			lines = append(lines, renderHeatmap(data, width, reference)...)
		default:
			lines = append(lines, renderCategories(kind, config, data.Rows, width)...)
		}
	case "single_value":
		for _, row := range data.Rows {
			label := ""
			if group, ok := row["group_value"]; ok {
				label = cell(group) + " · "
			}
			lines = append(lines, strings.TrimSpace(label+cell(row["value"])+" "+config.Unit))
		}
	case "table":
		lines = append(lines, renderTable(data, false, width)...)
	case "time_series":
		series, from, to, skipped := timeSeries(data.Rows)
		if skipped > 0 {
			lines = append(lines, fmt.Sprintf("%d null/invalid time or value rows omitted; query data preserved.", skipped))
		}
		fallback := false
		if len(series) == 0 {
			lines = append(lines, "No plottable timestamp/value pairs.")
			fallback = true
		} else if width < 32 {
			lines = append(lines, "Terminal too narrow for time-series chart; data follows.")
			fallback = true
		} else {
			if live {
				config.TimeWindow = 900
			}
			if config.TimeWindow > 0 {
				to = reference
				from = to.Add(-time.Duration(config.TimeWindow * float64(time.Second)))
				for i := range series {
					points := series[i].Points[:0]
					for _, p := range series[i].Points {
						if !p.Time.Before(from) && !p.Time.After(to) {
							points = append(points, p)
						}
					}
					series[i].Points = points
				}
			}
			labels := []string{}
			for _, s := range series {
				labels = append(labels, s.Name)
			}
			lines = append(lines, "Series: "+strings.Join(labels, " | "), "From: "+displayTime(from, reference), "To: "+displayTime(to, reference))
			lines = append(lines, strings.Split(charts.RenderTimeSeries(width, max(4, height-3), from, to, series, reference), "\n")...)
		}
		if fallback {
			lines = append(lines, renderTableAt(data, true, reference, width)...)
		}
	default:
		lines = append(lines, fmt.Sprintf("Visualization %q is unsupported in the terminal; showing ordered query data.", kind))
		lines = append(lines, renderTable(data, false, width)...)
	}
	return lines
}
func displayTime(t time.Time, now ...time.Time) string {
	return terminal.Timestamp(t.UnixNano(), "2006-01-02 15:04:05.000 MST", time.Local, terminal.BatchTime(now...))
}

func renderTable(data apigen.QueryPreview, sampleTimes bool, widths ...int) []string {
	return renderTableAt(data, sampleTimes, time.Now(), widths...)
}
func renderTableAt(data apigen.QueryPreview, sampleTimes bool, reference time.Time, widths ...int) []string {
	columns := append([]string(nil), data.Columns...)
	if len(columns) == 0 {
		seen := map[string]bool{}
		for _, row := range data.Rows {
			for name := range row {
				seen[name] = true
			}
		}
		for name := range seen {
			columns = append(columns, name)
		}
		sort.Strings(columns)
	}
	rows := make([][]string, 0, len(data.Rows))
	for _, row := range data.Rows {
		values := []string{}
		for _, name := range columns {
			value := cell(row[name])
			// Only the time-series renderer declares these aliases as sample
			// timestamps. Arbitrary table/fallback SQL columns retain their values.
			if sampleTimes {
				switch name {
				case "timestamp_ns", "time_ns", "timestamp", "time", "start_ns":
					if sampleTime, ok := timestamp(map[string]any{name: row[name]}); ok {
						value = displayTime(sampleTime, reference)
					}
				}
			}
			values = append(values, value)
		}
		rows = append(rows, values)
	}
	if len(widths) > 0 {
		return layoutTable(columns, rows, widths[0])
	}
	lines := []string{fmt.Sprintf("%d rows · columns: %s", len(rows), strings.Join(columns, " | "))}
	for i, row := range rows {
		values := []string{}
		for j, value := range row {
			values = append(values, columns[j]+"="+value)
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, strings.Join(values, " · ")))
	}
	return lines
}

// Result cells wrap instead of truncating. Wide terminals get aligned columns;
// narrow terminals get labeled records, preserving every SQL column and value.
func layoutTable(columns []string, rows [][]string, width int) []string {
	lines := []string{fmt.Sprintf("%d rows", len(rows))}
	if len(columns) == 0 {
		return lines
	}
	if width < 72 || len(columns) > 6 {
		for i, row := range rows {
			lines = append(lines, fmt.Sprintf("ROW %d", i+1))
			for j, value := range row {
				lines = append(lines, columns[j]+": "+value)
			}
			lines = append(lines, "")
		}
		return lines
	}
	columnWidths := make([]int, len(columns))
	capWidth := max(1, (width-3*(len(columns)-1))/len(columns))
	for i, name := range columns {
		columnWidths[i] = min(capWidth, max(1, ansi.StringWidth(safe(name))))
		for _, row := range rows {
			columnWidths[i] = max(columnWidths[i], min(capWidth, ansi.StringWidth(safe(row[i]))))
		}
	}
	appendRow := func(row []string) {
		cells := make([][]string, len(row))
		height := 1
		for i, value := range row {
			cells[i] = strings.Split(ansi.Hardwrap(safe(value), columnWidths[i], true), "\n")
			height = max(height, len(cells[i]))
		}
		for line := 0; line < height; line++ {
			parts := make([]string, len(row))
			for i := range row {
				text := ""
				if line < len(cells[i]) {
					text = cells[i][line]
				}
				parts[i] = text + strings.Repeat(" ", max(0, columnWidths[i]-ansi.StringWidth(text)))
			}
			lines = append(lines, strings.Join(parts, " │ "))
		}
	}
	appendRow(columns)
	separators := make([]string, len(columns))
	for i, size := range columnWidths {
		separators[i] = strings.Repeat("─", size)
	}
	lines = append(lines, strings.Join(separators, "─┼─"))
	for _, row := range rows {
		appendRow(row)
	}
	return lines
}
func cell(value any) string {
	if value == nil {
		return "NULL"
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	default:
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return fmt.Sprint(v)
	}
}
func number(v any) (float64, bool) {
	var n float64
	var err error
	switch x := v.(type) {
	case float64:
		n = x
	case int64:
		n = float64(x)
	case int:
		n = float64(x)
	case json.Number:
		n, err = x.Float64()
	case string:
		if strings.TrimSpace(x) == "" {
			return 0, false
		}
		n, err = strconv.ParseFloat(strings.TrimSpace(x), 64)
	default:
		return 0, false
	}
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func timestamp(row map[string]any) (time.Time, bool) {
	for _, key := range []string{"timestamp_ns", "time_ns", "timestamp", "time", "start_ns"} {
		v, exists := row[key]
		if !exists || v == nil {
			continue
		}
		s, exact := v.(string)
		if number, ok := v.(json.Number); ok {
			s, exact = number.String(), true
		}
		if integer, ok := v.(int64); ok {
			s, exact = strconv.FormatInt(integer, 10), true
		}
		if exact {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t, true
			}
			if integer, err := strconv.ParseInt(s, 10, 64); err == nil {
				if key != "timestamp" && key != "time" || integer > 1e14 || integer < -1e14 {
					return time.Unix(0, integer), true
				}
				return time.UnixMilli(integer), true
			}
		}
		n, ok := number(v)
		if !ok {
			return time.Time{}, false
		}
		if key != "timestamp" && key != "time" || math.Abs(n) > 1e14 {
			if n >= float64(math.MaxInt64) || n < float64(math.MinInt64) {
				return time.Time{}, false
			}
			return time.Unix(0, int64(n)), true
		}
		return time.UnixMilli(int64(n)), true
	}
	return time.Time{}, false
}
func timeSeries(rows []map[string]any) ([]charts.Series, time.Time, time.Time, int) {
	grouped := map[string][]charts.Point{}
	var from, to time.Time
	skipped := 0
	for _, row := range rows {
		t, ok := timestamp(row)
		value, numeric := number(row["value"])
		if !ok || !numeric {
			skipped++
			continue
		}
		group := "value"
		if x, exists := row["group_value"]; exists {
			group = cell(x)
		}
		grouped[group] = append(grouped[group], charts.Point{Time: t, Value: value})
		if from.IsZero() || t.Before(from) {
			from = t
		}
		if to.IsZero() || t.After(to) {
			to = t
		}
	}
	names := []string{}
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	series := []charts.Series{}
	for _, name := range names {
		series = append(series, charts.Series{Name: name, Points: grouped[name]})
	}
	return series, from, to, skipped
}
