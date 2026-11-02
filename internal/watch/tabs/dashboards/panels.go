package dashboards

import (
	"encoding/json"
	"fmt"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/charts"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type settings struct {
	Unit       string  `json:"unit"`
	TimeWindow float64 `json:"time_window_seconds"`
}

func renderPanel(kind, raw string, data apigen.QueryPreview, width, height int, live bool) []string {
	var config settings
	_ = json.Unmarshal([]byte(raw), &config)
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
	case "single_value":
		for _, row := range data.Rows {
			label := "value"
			if group, ok := row["group_value"]; ok {
				label = cell(group)
			}
			lines = append(lines, label+": "+cell(row["value"])+" "+config.Unit)
		}
	case "table":
		lines = append(lines, renderTable(data)...)
	case "time_series":
		series, from, to, skipped := timeSeries(data.Rows)
		if skipped > 0 {
			lines = append(lines, fmt.Sprintf("%d null/invalid time or value rows omitted from chart (available in data below).", skipped))
		}
		if len(series) == 0 {
			lines = append(lines, "No plottable timestamp/value pairs.")
		} else if width < 32 {
			lines = append(lines, "Terminal too narrow for time-series chart; data follows.")
		} else {
			if live {
				config.TimeWindow = 900
			}
			if config.TimeWindow > 0 {
				to = time.Now()
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
			lines = append(lines, "Series: "+strings.Join(labels, " | "), from.Format(time.RFC3339)+" → "+to.Format(time.RFC3339))
			lines = append(lines, strings.Split(charts.RenderTimeSeries(width, min(12, max(4, height-4)), from, to, series), "\n")...)
		}
		lines = append(lines, "Query data:")
		lines = append(lines, renderTable(data)...)
	default:
		lines = append(lines, fmt.Sprintf("Visualization %q is unsupported in the terminal; showing ordered query data.", kind))
		lines = append(lines, renderTable(data)...)
	}
	return lines
}
func renderTable(data apigen.QueryPreview) []string {
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
	lines := []string{fmt.Sprintf("%d rows · columns: %s", len(data.Rows), strings.Join(columns, " | "))}
	for i, row := range data.Rows {
		values := []string{}
		for _, name := range columns {
			values = append(values, name+"="+cell(row[name]))
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, strings.Join(values, " · ")))
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
		n, err = strconv.ParseFloat(x, 64)
	default:
		return 0, false
	}
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func timestamp(row map[string]any) (time.Time, bool) {
	for _, key := range []string{"timestamp_ns", "time_ns", "timestamp", "start_ns"} {
		v, exists := row[key]
		if !exists || v == nil {
			continue
		}
		s, exact := v.(string)
		if number, ok := v.(json.Number); ok {
			s, exact = number.String(), true
		}
		if exact {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t, true
			}
			if ns, err := strconv.ParseInt(s, 10, 64); err == nil && key != "timestamp" {
				return time.Unix(0, ns), true
			}
		}
		n, ok := number(v)
		if !ok {
			return time.Time{}, false
		}
		if key != "timestamp" || math.Abs(n) > 1e14 {
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
