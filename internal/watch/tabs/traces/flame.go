package traces

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/tables"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

type flameBar struct {
	row                         flatSpan
	start, end                  int64
	left, right                 int // Half-open terminal-cell range.
	incomplete, point, mismatch bool
}
type flameLane struct {
	depth, sub int
	bars       []flameBar
}
type flameLayout struct {
	start, end                                       int64
	lanes                                            []flameLane
	invalid, incomplete, points, orphans, mismatches int
}

type flameLaneEnd struct{ right, index int }
type flameEnds []flameLaneEnd

func (h flameEnds) Len() int { return len(h) }
func (h flameEnds) Less(i, j int) bool {
	if h[i].right != h[j].right {
		return h[i].right < h[j].right
	}
	return h[i].index < h[j].index
}
func (h flameEnds) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *flameEnds) Push(v any)   { *h = append(*h, v.(flameLaneEnd)) }
func (h *flameEnds) Pop() any     { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

// Use recorded start/end positions. A missing end is a start marker, never an
// inferred duration. Invalid timestamps remain in the waterfall/inspector.
func layoutFlame(rows []flatSpan, width int) flameLayout {
	width = max(1, width)
	var layout flameLayout
	var bars []flameBar
	for _, row := range rows {
		s := row.span
		if s.StartNs <= 0 || s.EndNs != 0 && s.EndNs < s.StartNs {
			layout.invalid++
			continue
		}
		bar := flameBar{row: row, start: s.StartNs, end: s.EndNs, incomplete: s.EndNs == 0}
		if bar.incomplete {
			bar.end = bar.start
			layout.incomplete++
		} else {
			bar.point = bar.end == bar.start
			bar.mismatch = s.DurationNs != bar.end-bar.start
			if bar.point {
				layout.points++
			}
			if bar.mismatch {
				layout.mismatches++
			}
		}
		if row.orphan {
			layout.orphans++
		}
		if layout.start == 0 || bar.start < layout.start {
			layout.start = bar.start
		}
		layout.end = max(layout.end, bar.end)
		bars = append(bars, bar)
	}
	window := max(int64(1), layout.end-layout.start)
	for i := range bars {
		bar := &bars[i]
		bar.left = max(0, min(width-1, int(math.Floor(float64(bar.start-layout.start)/float64(window)*float64(width)))))
		bar.right = max(bar.left+1, min(width, int(math.Ceil(float64(bar.end-layout.start)/float64(window)*float64(width)))))
	}
	sort.SliceStable(bars, func(i, j int) bool {
		a, b := bars[i], bars[j]
		if a.row.depth != b.row.depth {
			return a.row.depth < b.row.depth
		}
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end < b.end
		}
		return spanKey(a.row.span) < spanKey(b.row.span)
	})
	// Split intersecting bars at the same depth into sub-lanes. This also
	// preserves spans that collide only at the terminal's pixel resolution.
	var available flameEnds
	depth, sub := 0, 0
	for i, bar := range bars {
		if i == 0 || depth != bar.row.depth {
			available = nil
			depth = bar.row.depth
			sub = 0
		}
		laneIndex := -1
		if len(available) > 0 && available[0].right <= bar.left {
			laneIndex = heap.Pop(&available).(flameLaneEnd).index
		}
		if laneIndex < 0 {
			layout.lanes = append(layout.lanes, flameLane{depth: bar.row.depth, sub: sub})
			laneIndex = len(layout.lanes) - 1
			sub++
		}
		layout.lanes[laneIndex].bars = append(layout.lanes[laneIndex].bars, bar)
		heap.Push(&available, flameLaneEnd{bar.right, laneIndex})
	}
	return layout
}

func renderFlameBar(bar flameBar, selected bool) string {
	width := bar.right - bar.left
	text := ""
	if width == 1 {
		switch {
		case selected:
			text = ">"
		case bar.incomplete:
			text = "!"
		case bar.point:
			text = terminal.Glyphs().Bullet
		default:
			text = "█"
			if terminal.Glyphs().Pipe == "|" {
				text = "#"
			}
		}
	} else {
		name := clean(bar.row.span.Name)
		if name == "" {
			name = "(unnamed)"
		}
		if width >= 24 && bar.row.span.ServiceName != "" {
			name += " / " + clean(bar.row.span.ServiceName)
		}
		if bar.row.orphan {
			name = "? " + name
		}
		if bar.incomplete || bar.mismatch {
			name = "! " + name
		}
		label := terminal.Truncate(name, width-2)
		left := "["
		if selected {
			left = ">"
		}
		text = left + label + strings.Repeat(terminal.Glyphs().Rule, max(0, width-2-ansi.StringWidth(label))) + "]"
	}
	return tui.RenderContent(tui.ServiceBlockStyle(bar.row.span.ServiceName, selected), text)
}

func renderFlame(rows []flatSpan, selected string, width, height int) []string {
	if height <= 0 {
		return nil
	}
	if width < 20 || height < 3 {
		return []string{tui.RenderContent(tui.ContentContext, terminal.Truncate("Flame chart · expand terminal for depth lanes", max(1, width)))}
	}
	const labelWidth = 5
	plotWidth := width - labelWidth - 1
	layout := layoutFlame(rows, plotWidth)
	lines := []string{tui.RenderContent(tui.ContentTitle, terminal.Truncate("Flame chart · actual start/end", width))}
	if len(layout.lanes) == 0 {
		return append(lines, tui.RenderContent(tui.ContentContext, terminal.Truncate("No placeable timing; full spans remain below.", width)))
	}
	selectedLane, selectedFound := 0, false
	for i, lane := range layout.lanes {
		for _, bar := range lane.bars {
			if spanKey(bar.row.span) == selected {
				selectedLane = i
				selectedFound = true
			}
		}
	}
	var notes []string
	if layout.invalid > 0 {
		notes = append(notes, fmt.Sprintf("%d unplaced timing", layout.invalid))
	}
	if layout.incomplete > 0 {
		notes = append(notes, fmt.Sprintf("!%d end unknown", layout.incomplete))
	}
	if layout.points > 0 {
		notes = append(notes, fmt.Sprintf("%s%d zero duration", terminal.Glyphs().Bullet, layout.points))
	}
	if layout.orphans > 0 {
		notes = append(notes, fmt.Sprintf("?%d orphan/cyclic", layout.orphans))
	}
	if layout.mismatches > 0 {
		notes = append(notes, fmt.Sprintf("%d reported duration differs", layout.mismatches))
	}
	if selected != "" && !selectedFound {
		notes = append(notes, "selected timing unavailable; inspect below")
	}
	ruler := height >= 5
	base := 1
	if ruler {
		base++
	}
	laneBudget := height - base
	if len(notes) > 0 || len(layout.lanes) > laneBudget {
		laneBudget--
	}
	laneBudget = max(1, laneBudget)

	start := max(0, min(selectedLane-laneBudget/2, len(layout.lanes)-laneBudget))
	end := min(len(layout.lanes), start+laneBudget)
	hidden := len(layout.lanes) - (end - start)
	if hidden > 0 {
		message := fmt.Sprintf("%d packed lanes hidden", hidden)
		if selectedFound {
			message += "; selection shown"
		}
		notes = append([]string{message}, notes...)
	}
	style := func(row, col int) lipgloss.Style {
		if col == 0 {
			return tui.ContentLabel
		}
		return tui.ContentValue
	}
	if ruler {
		right := terminal.Truncate(duration(layout.end-layout.start), max(1, plotWidth-2))
		axis := "0" + strings.Repeat(" ", max(1, plotWidth-1-ansi.StringWidth(right))) + right
		lines = append(lines, tables.Compact(nil, [][]string{{"", axis}}, []int{labelWidth, plotWidth}, style)[0])
	} else {
		lines[0] = tui.RenderContent(tui.ContentTitle, terminal.Truncate("Flame chart · 0 → "+duration(layout.end-layout.start), width))
	}
	for _, lane := range layout.lanes[start:end] {
		var plot strings.Builder
		cursor := 0
		for _, bar := range lane.bars {
			plot.WriteString(strings.Repeat(" ", max(0, bar.left-cursor)))
			plot.WriteString(renderFlameBar(bar, spanKey(bar.row.span) == selected))
			cursor = bar.right
		}
		label := fmt.Sprintf("d%d", lane.depth)
		if lane.sub > 0 {
			label += fmt.Sprintf(".%d", lane.sub+1)
		}
		label = terminal.Truncate(label, labelWidth)
		lines = append(lines, tables.Compact(nil, [][]string{{label, plot.String()}}, []int{labelWidth, plotWidth}, style)[0])
	}
	if len(notes) > 0 {
		lines = append(lines, tui.RenderContent(tui.ContentContext, terminal.Truncate(strings.Join(notes, " · "), width)))
	}
	return lines[:min(len(lines), height)]
}
