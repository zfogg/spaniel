package traces

import (
	"math"
	"sort"
	"strings"

	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type flatSpan struct {
	span   apigen.Span
	depth  int
	orphan bool
}

func spanKey(s apigen.Span) string { return s.SessionId + "/" + s.SpanId }
func dedupeSpans(spans []apigen.Span) []apigen.Span {
	byID := map[string]apigen.Span{}
	for _, s := range spans {
		if s.SpanId != "" {
			byID[spanKey(s)] = s
		}
	}
	out := make([]apigen.Span, 0, len(byID))
	for _, s := range byID {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartNs != out[j].StartNs {
			return out[i].StartNs < out[j].StartNs
		}
		return spanKey(out[i]) < spanKey(out[j])
	})
	return out
}
func rootParent(id string) bool { return id == "" || id == "0000000000000000" }

// Like the website, parents precede their time-ordered children and missing
// parents become roots. The iterative walk also retains cyclic and self-parented
// input without recursion, omitted rows, or unbounded indentation.
func flatten(spans []apigen.Span) []flatSpan {
	ordered := dedupeSpans(spans)
	byID := map[string]bool{}
	for _, s := range ordered {
		byID[spanKey(s)] = true
	}
	children := map[string][]apigen.Span{}
	var roots []apigen.Span
	for _, s := range ordered {
		parent := s.SessionId + "/" + s.ParentSpanId
		if rootParent(s.ParentSpanId) || !byID[parent] || parent == spanKey(s) {
			roots = append(roots, s)
		} else {
			children[parent] = append(children[parent], s)
		}
	}
	seen := map[string]bool{}
	out := make([]flatSpan, 0, len(ordered))
	walk := func(root apigen.Span, cycle bool) {
		stack := []flatSpan{{span: root, orphan: cycle || (!rootParent(root.ParentSpanId) && (!byID[root.SessionId+"/"+root.ParentSpanId] || root.ParentSpanId == root.SpanId))}}
		for len(stack) > 0 {
			row := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			id := spanKey(row.span)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, row)
			kids := children[id]
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, flatSpan{span: kids[i], depth: row.depth + 1})
			}
		}
	}
	for _, root := range roots {
		walk(root, false)
	}
	for _, s := range ordered {
		if !seen[spanKey(s)] {
			walk(s, true)
		}
	}
	return out
}

// Missing ends can use the reported duration; invalid ranges never produce
// negative repetition counts or overflow. A zero-duration span stays visible.
func timing(s apigen.Span) (int64, int64, bool) {
	if s.StartNs <= 0 || s.DurationNs < 0 || (s.EndNs != 0 && s.EndNs < s.StartNs) {
		return 0, 0, false
	}
	end := s.EndNs
	if end == 0 {
		if s.DurationNs > math.MaxInt64-s.StartNs {
			return 0, 0, false
		}
		end = s.StartNs + s.DurationNs
	}
	return s.StartNs, end, true
}
func traceWindow(rows []flatSpan) (int64, int64) {
	var start, end int64
	for _, r := range rows {
		a, b, ok := timing(r.span)
		if !ok {
			continue
		}
		if start == 0 || a < start {
			start = a
		}
		if b > end {
			end = b
		}
	}
	if end < start {
		end = start
	}
	return start, end
}
func waterfallBar(s apigen.Span, start, end int64, width int) string {
	width = max(1, width)
	a, _, ok := timing(s)
	if !ok {
		return "!" + strings.Repeat(" ", width-1)
	}
	window := max(int64(1), end-start)
	offset := int(float64(a-start) / float64(window) * float64(width))
	offset = max(0, min(width-1, offset))
	// Duration is the same reported measure used by the website's computeLayout.
	length := int(math.Min(float64(width), float64(max(int64(0), s.DurationNs))/float64(window)*float64(width)))
	length = max(1, min(width-offset, length))
	return strings.Repeat(" ", offset) + strings.Repeat("█", length) + strings.Repeat(" ", width-offset-length)
}
func waterfallRow(row flatSpan, selected bool, start, end int64, width int) string {
	s := row.span
	prefix := "  "
	if selected {
		prefix = "› "
	}
	mark := ""
	if row.orphan {
		mark = "? "
	}
	if _, _, ok := timing(s); !ok || s.EndNs == 0 {
		mark += "! "
	}
	labelPrefix := strings.Repeat("  ", min(row.depth, 12)) + mark + clean(s.Name) + " / "
	status := statusText(s.StatusCode)
	line := prefix + tui.RenderContent(statusStyle(s.StatusCode), status) + " · " + serviceLabel(labelPrefix, s.ServiceName, "", 0) + " · " + tui.RenderContent(tui.ContentMeasure, duration(s.DurationNs))
	if width >= 62 {
		labelWidth := max(16, min(52, width/2))
		barWidth := max(1, width-labelWidth-24)
		edge := terminal.Glyphs().Pipe
		barStyle := tui.ContentChart
		if s.StatusCode >= 2 {
			barStyle = tui.ContentError
		}
		line = prefix + serviceLabel(labelPrefix, s.ServiceName, "", labelWidth) + " " + tui.RenderContent(tui.ContentMeasure, pad(duration(s.DurationNs), 10)) + " " + tui.RenderContent(statusStyle(s.StatusCode), pad(status, 5)) + " " + tui.RenderContent(tui.ContentRule, edge) + tui.RenderContent(barStyle, waterfallBar(s, start, end, barWidth)) + tui.RenderContent(tui.ContentRule, edge)
	}
	line = clip(line, width)
	return tui.ContentRow(line, selected)
}
