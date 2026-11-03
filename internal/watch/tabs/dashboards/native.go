package dashboards

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type category struct {
	label, raw, context string
	service             string
	value               float64
}

func column(row map[string]any, names ...string) any {
	for _, name := range names {
		if name == "" {
			continue
		}
		if value, ok := row[name]; ok && value != nil {
			return value
		}
	}
	return nil
}

func categoryRows(kind string, config settings, rows []map[string]any) ([]category, int) {
	var out []category
	invalid := 0
	for _, row := range rows {
		value := column(row, "value")
		if kind == "entity_list" {
			value = column(row, config.Columns["value"], "primary_value", "value", "duration_ns")
		}
		n, ok := number(value)
		if !ok {
			invalid++
			continue
		}
		label := column(row, config.Columns["label"], "label", "group_value", "service_name", "name")
		context := ""
		if kind == "entity_list" {
			seen := map[string]bool{}
			for _, key := range []string{config.Columns["secondary"], config.Columns["badge"], "status", "state"} {
				if key != "" && !seen[key] && row[key] != nil {
					seen[key] = true
					context += " · " + safe(cell(row[key]))
				}
			}
		}
		service := ""
		for _, key := range []string{"service_name", "service.name", "service"} {
			if row[key] != nil && cell(row[key]) == cell(label) {
				service = cell(row[key])
				break
			}
		}
		out = append(out, category{label: safe(cell(label)), raw: safe(cell(value)), context: context, value: n, service: service})
	}
	return out, invalid
}

func renderCategories(kind string, config settings, rows []map[string]any, width int) []string {
	values, invalid := categoryRows(kind, config, rows)
	var lines []string
	if invalid > 0 {
		lines = append(lines, fmt.Sprintf("Warning: %d null/invalid numeric rows omitted; query data retained in inspection.", invalid))
	}
	if len(values) == 0 {
		return append(lines, "No numeric values to display.")
	}
	pie := kind == "pie"
	if pie || kind == "entity_list" {
		sort.SliceStable(values, func(i, j int) bool { return values[i].value > values[j].value })
	}
	scale, minV, maxV := 1.0, 0.0, 0.0
	for _, row := range values {
		if pie && row.value < 0 {
			return append(lines, "Invalid pie: proportions require non-negative values; inspect query data for the negative rows.")
		}
		scale = math.Max(scale, math.Abs(row.value))
	}
	total := 0.0
	for _, row := range values {
		total += row.value / scale
		minV, maxV = math.Min(minV, row.value/scale), math.Max(maxV, row.value/scale)
	}
	if pie && total <= 0 {
		return append(lines, "No positive values to display as proportions.")
	}
	barWidth := max(1, min(72, width-4))
	if pie {
		lines = append(lines, "Proportional bars · total "+categoryTotal(values)+" "+safe(config.Unit))
	} else {
		lines = append(lines, fmt.Sprintf("Signed bars · zero %s · range %g → %g %s", terminal.Glyphs().Pipe, minV*scale, maxV*scale, safe(config.Unit)))
	}
	for i, row := range values {
		label := tui.RenderContent(tui.ContentStrong, row.label)
		if row.service != "" {
			label = tui.RenderServiceName(row.service, row.label)
		}
		if kind == "entity_list" || pie {
			label = fmt.Sprintf("%d. %s", i+1, label)
		}
		text := fmt.Sprintf("%s  %s %s", label, tui.RenderContent(tui.ContentMeasure, row.raw), safe(config.Unit))
		bar := []rune(strings.Repeat(" ", barWidth))
		if pie {
			share := row.value / scale / total
			text += fmt.Sprintf("  (%.1f%%)", share*100)
			for x := 0; x < min(barWidth, int(math.Round(share*float64(barWidth)))); x++ {
				bar[x] = '█'
			}
		} else {
			rangeV := maxV - minV
			if rangeV == 0 {
				rangeV = 1
			}
			zero := int(math.Round(-minV / rangeV * float64(barWidth-1)))
			end := int(math.Round((row.value/scale - minV) / rangeV * float64(barWidth-1)))
			for x := min(zero, end); x <= max(zero, end); x++ {
				bar[x] = '█'
			}
			bar[zero] = []rune(terminal.Glyphs().Pipe)[0]
		}
		lines = append(lines, text+tui.RenderContent(tui.ContentContext, row.context), tui.RenderContent(tui.ContentChart, "  "+string(bar)))
	}
	return lines
}

// Finite decimal query values have exact rational sums even when their total
// exceeds float64. Only the bar geometry and displayed percentages use doubles.
func categoryTotal(rows []category) string {
	total := new(big.Rat)
	for _, row := range rows {
		value, ok := new(big.Rat).SetString(row.raw)
		if !ok {
			return "unavailable"
		}
		total.Add(total, value)
	}
	if total.IsInt() {
		return total.Num().String()
	}
	denominator := new(big.Int).Set(total.Denom())
	digits := 0
	for _, factor := range []int64{2, 5} {
		count := 0
		for denominator.Cmp(big.NewInt(1)) > 0 {
			q, r := new(big.Int), new(big.Int)
			q.QuoRem(denominator, big.NewInt(factor), r)
			if r.Sign() != 0 {
				break
			}
			denominator = q
			count++
		}
		digits = max(digits, count)
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return total.RatString()
	}
	return strings.TrimRight(strings.TrimRight(total.FloatString(digits), "0"), ".")
}

func renderGauge(config settings, rows []map[string]any, width int) []string {
	if len(rows) == 0 {
		return []string{"No numeric value to display."}
	}
	minV, maxV := 0.0, 100.0
	var ok bool
	if config.Min != nil {
		if minV, ok = number(config.Min); !ok {
			return []string{"Invalid gauge minimum; inspect settings/query data."}
		}
	}
	if config.Max != nil {
		if maxV, ok = number(config.Max); !ok {
			return []string{"Invalid gauge maximum; inspect settings/query data."}
		}
	}
	if maxV <= minV || math.IsInf(maxV-minV, 0) {
		return []string{"Invalid gauge range: maximum must exceed minimum with a finite range."}
	}
	value, ok := number(rows[0]["value"])
	if !ok {
		return []string{"No numeric value to display."}
	}
	fraction := math.Max(0, math.Min(1, (value-minV)/(maxV-minV)))
	size := max(1, min(72, width-2))
	fill := int(math.Round(fraction * float64(size)))
	lines := []string{"Value: " + cell(rows[0]["value"]) + " " + safe(config.Unit), "[" + strings.Repeat("█", fill) + strings.Repeat("░", size-fill) + "]", fmt.Sprintf("Range: %g → %g %s", minV, maxV, safe(config.Unit))}
	if value < minV || value > maxV {
		lines = append(lines, "Out of range: meter fill clamped; actual value shown above.")
	}
	ticks := []rune(strings.Repeat(" ", size))
	var thresholdLines []string
	for _, item := range config.Thresholds {
		label, raw := "threshold", item
		if threshold, ok := item.(map[string]any); ok {
			raw = threshold["value"]
			if threshold["label"] != nil {
				label = safe(cell(threshold["label"]))
			}
		}
		threshold, ok := number(raw)
		if !ok {
			thresholdLines = append(thresholdLines, "Warning: invalid gauge threshold omitted.")
			continue
		}
		state := "below"
		if value >= threshold {
			state = "reached"
		}
		if threshold >= minV && threshold <= maxV {
			x := int(math.Round((threshold - minV) / (maxV - minV) * float64(size-1)))
			ticks[x] = '┆'
		} else {
			state += " · outside meter range"
		}
		thresholdLines = append(thresholdLines, fmt.Sprintf("Threshold %s: %s %s · %s", label, cell(raw), safe(config.Unit), state))
	}
	if len(config.Thresholds) > 0 {
		lines = append(lines, " "+string(ticks)+" ", "┆ threshold positions")
	}
	lines = append(lines, thresholdLines...)
	return lines
}

func renderHeatmap(data apigen.QueryPreview, width int, now ...time.Time) []string {
	reference := terminal.BatchTime(now...)
	values := map[[2]string]float64{}
	xLabels, yLabels := map[string]string{}, map[string]string{}
	xOrder, yOrder := map[string]float64{}, map[string]float64{}
	xTimes := map[string]time.Time{}
	temporal, duration, invalid := false, false, 0
	for _, row := range data.Rows {
		x, y := column(row, "timestamp_ns", "timestamp", "time_ns", "time", "x"), column(row, "bucket_ms", "y")
		v, ok := number(row["value"])
		if x == nil || y == nil || !ok || v < 0 {
			invalid++
			continue
		}
		xKey, yKey := cell(x), cell(y)
		if column(row, "timestamp_ns", "timestamp", "time_ns", "time") != nil {
			t, ok := timestamp(row)
			if !ok {
				invalid++
				continue
			}
			temporal = true
			xKey = t.UTC().Format(time.RFC3339Nano)
			xTimes[xKey] = t
			xLabels[xKey], xOrder[xKey] = terminal.Timestamp(t.UnixNano(), "15:04:05.000 MST", time.UTC, reference), float64(t.UnixNano())
		} else {
			if _, numeric := x.(string); !numeric {
				if _, valid := number(x); !valid {
					invalid++
					continue
				}
			}
			xLabels[xKey] = safe(xKey)
			if n, ok := number(x); ok {
				xOrder[xKey] = n
			}
		}
		if row["bucket_ms"] != nil {
			n, valid := number(y)
			if !valid || n < 0 {
				invalid++
				continue
			}
			duration = true
			yLabels[yKey], yOrder[yKey] = fmt.Sprintf("%g ms", n), n
		} else {
			if _, text := y.(string); !text {
				if _, valid := number(y); !valid {
					invalid++
					continue
				}
			}
			yLabels[yKey] = safe(yKey)
			if n, ok := number(y); ok {
				yOrder[yKey] = n
			}
		}
		key := [2]string{xKey, yKey}
		values[key] += v
		if math.IsInf(values[key], 0) {
			return append([]string{"Heatmap count overflow; inspect exact query values."}, renderTableAt(data, true, reference, width)...)
		}
	}
	var lines []string
	if invalid > 0 {
		lines = append(lines, fmt.Sprintf("Warning: %d invalid/negative heatmap rows omitted; query data retained in inspection.", invalid))
	}
	if len(values) == 0 {
		return append(lines, "No valid heatmap coordinates/counts to display.")
	}
	// Derive axes only from admitted cells, including explicit zero counts.
	xSet, ySet := map[string]bool{}, map[string]bool{}
	maxV := 0.0
	for key, value := range values {
		xSet[key[0]], ySet[key[1]] = true, true
		maxV = math.Max(maxV, value)
	}
	ordered := func(set map[string]bool, numbers map[string]float64) []string {
		keys := make([]string, 0, len(set))
		for key := range set {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, aOK := numbers[keys[i]]
			b, bOK := numbers[keys[j]]
			if aOK && bOK && a != b {
				return a < b
			}
			return keys[i] < keys[j]
		})
		return keys
	}
	xs, ys := ordered(xSet, xOrder), ordered(ySet, yOrder)
	if temporal {
		sort.SliceStable(xs, func(i, j int) bool {
			a, aOK := xTimes[xs[i]]
			b, bOK := xTimes[xs[j]]
			if aOK && bOK {
				return a.Before(b)
			}
			return xs[i] < xs[j]
		})
	}
	labelWidth := min(18, max(8, width/5))
	if width < 28 || len(xs) > 64 || len(ys) > 32 || len(xs)*len(ys) > 2048 || labelWidth+3*len(xs)+3 > width {
		return append(append(lines, "Heatmap grid exceeds terminal width/bucket bounds; labeled query cells follow."), renderTableAt(data, true, reference, width)...)
	}
	caption := "X buckets → · Y buckets ↑"
	if temporal {
		caption = "UTC time buckets → · Y buckets ↑"
	}
	if duration {
		caption += " · duration lower bounds (ms)"
	}
	lines = append(lines, caption)
	pad := func(s string, size int) string {
		s = ansi.Truncate(s, size, "…")
		return s + strings.Repeat(" ", max(0, size-ansi.StringWidth(s)))
	}
	header := pad("Y / X", labelWidth) + " " + terminal.Glyphs().Pipe
	for i := range xs {
		header += fmt.Sprintf("%3d", i+1)
	}
	lines = append(lines, header)
	shades := []rune("░▒▓█")
	for i := len(ys) - 1; i >= 0; i-- {
		y := ys[i]
		line := pad(yLabels[y], labelWidth) + " " + terminal.Glyphs().Pipe
		for _, x := range xs {
			v, present := values[[2]string{x, y}]
			glyph := '·'
			if present {
				glyph = '0'
				if v > 0 {
					glyph = shades[min(3, int(math.Ceil(4*math.Sqrt(v/maxV)))-1)]
				}
			}
			line += " " + string(glyph) + " "
		}
		lines = append(lines, line)
	}
	lines = append(lines, fmt.Sprintf("Intensity ░▒▓█: sqrt(count), 0 → %g; 0 = zero; · = no data", maxV))
	for i, x := range xs {
		lines = append(lines, fmt.Sprintf("X%d = %s", i+1, xLabels[x]))
	}
	return lines
}
