package traces

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/servicecolor"
)

func flameSpan(id, parent, service, name string, start, end int64) apigen.Span {
	return apigen.Span{SessionId: "session", TraceId: "trace", SpanId: id, ParentSpanId: parent, ServiceName: service, Name: name, StartNs: start, EndNs: end, DurationNs: max(int64(0), end-start)}
}

func TestFlameUsesActualTimingAndRetainsEveryPlaceableSpan(t *testing.T) {
	base := int64(math.MaxInt64 - 10000)
	spans := []apigen.Span{
		flameSpan("root", "", "api", "request", base, base+1000),
		flameSpan("a", "root", "database", "query", base, base+500),
		flameSpan("b", "root", "worker", "parallel task", base+250, base+750),
		flameSpan("nested", "a", "database", "nested", base+100, base+200),
		flameSpan("zero", "nested", "worker", "instant", base+150, base+150),
		flameSpan("unknown", "b", "worker", "unfinished", base+600, 0),
		flameSpan("orphan", "missing", "api", "detached", base+800, base+900),
		flameSpan("missing-start", "root", "api", "unknown start", 0, base+1000),
		flameSpan("reversed", "root", "api", "bad end", base+500, base+100),
	}
	spans[1].DurationNs = 5000
	spans[5].DurationNs = 9000000
	before := append([]apigen.Span(nil), spans...)
	rows := flatten(spans)
	layout := layoutFlame(rows, 100)
	if layout.start != base || layout.end != base+1000 || layout.invalid != 2 || layout.incomplete != 1 || layout.points != 1 || layout.orphans != 1 || layout.mismatches != 1 {
		t.Fatalf("incorrect timing classification: %+v", layout)
	}
	seen := map[string]flameBar{}
	for _, lane := range layout.lanes {
		cursor := 0
		for _, bar := range lane.bars {
			if bar.left < cursor || bar.left < 0 || bar.right > 100 || bar.right <= bar.left {
				t.Fatal("overwritten/invalid flame cells", lane)
			}
			if _, duplicate := seen[bar.row.span.SpanId]; duplicate {
				t.Fatal("duplicated span")
			}
			seen[bar.row.span.SpanId] = bar
			cursor = bar.right
		}
	}
	if len(seen) != 7 || seen["a"].right != 50 || seen["nested"].left != 10 || seen["unknown"].right-seen["unknown"].left != 1 {
		t.Fatal("reported durations or large epochs distorted actual positions", seen)
	}
	if !reflect.DeepEqual(before, spans) {
		t.Fatal("flame layout mutated telemetry")
	}
}

func TestFlameBoundsSelectionSafetyAndCapabilities(t *testing.T) {
	profile, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); lipgloss.SetHasDarkBackground(dark) })
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SPANIEL_ASCII", "0")
	lipgloss.SetHasDarkBackground(true)
	const canonical = " 服务-🚀 full original "
	var spans []apigen.Span
	for i := 0; i < 20; i++ {
		parent := ""
		if i > 0 {
			parent = fmt.Sprint(i - 1)
		}
		spans = append(spans, flameSpan(fmt.Sprint(i), parent, canonical, "safe\x1b]0;bad\a\n界", 1000, 2000))
	}
	rows := flatten(spans)
	selected := spanKey(spans[len(spans)-1])
	for _, depth := range []termenv.Profile{termenv.TrueColor, termenv.ANSI256, termenv.ANSI, termenv.Ascii} {
		lipgloss.SetColorProfile(depth)
		for _, width := range []int{1, 12, 20, 40, 80, 120} {
			for _, height := range []int{0, 1, 3, 4, 6} {
				lines := renderFlame(rows, selected, width, height)
				if len(lines) > height {
					t.Fatal("flame exceeded vertical budget", width, height)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > width || strings.ContainsAny(ansi.Strip(line), "\r\n\a") || strings.Contains(ansi.Strip(line), "\x1b") {
						t.Fatal("unsafe/overflowing chart", width, height, line)
					}
				}
				text := strings.Join(lines, "\n")
				if width >= 20 && height >= 3 && (!strings.Contains(ansi.Strip(text), ">") || !strings.Contains(text, "lanes")) {
					t.Fatal("selected deep span not retained in bounded lanes", text)
				}
				if depth != termenv.TrueColor && strings.Contains(text, "38;2;") {
					t.Fatal("flame forced truecolor", text)
				}
			}
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	bar := layoutFlame(rows, 40).lanes[0].bars[0]
	color := servicecolor.Color(canonical).Text(true)
	styled := renderFlameBar(bar, false)
	if !strings.Contains(styled, fmt.Sprintf("38;2;%d;%d;%d", color.R, color.G, color.B)) {
		t.Fatal("bar hash used clipped/sanitized label", styled)
	}
	t.Setenv("NO_COLOR", "1")
	plain := renderFlameBar(bar, false)
	if plain != ansi.Strip(styled) || strings.Contains(plain, "\x1b") {
		t.Fatal("plain bar changed data or kept ANSI", plain)
	}
}

func TestFlamePointsAndUnavailableSelectionAreHonest(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	zero := flameSpan("zero", "", "api", "instant", 100, 100)
	unknown := flameSpan("unknown", "", "api", "no end", 200, 0)
	unknown.DurationNs = 9000
	layout := layoutFlame(flatten([]apigen.Span{zero, unknown}), 20)
	if layout.end != 200 || layout.points != 1 || layout.incomplete != 1 {
		t.Fatal("invented unknown duration", layout)
	}
	for _, lane := range layout.lanes {
		for _, bar := range lane.bars {
			if bar.right-bar.left != 1 {
				t.Fatal("point became a duration", bar)
			}
		}
	}
	invalid := flameSpan("invalid", "", "api", "no start", 0, 0)
	view := strings.Join(renderFlame(flatten([]apigen.Span{zero, unknown, invalid}), spanKey(invalid), 100, 6), "\n")
	if strings.Contains(view, "selection shown") || !strings.Contains(view, "selected timing unavailable") {
		t.Fatal("unplaceable selection claimed as visible", view)
	}
}

func TestFlameShortBarsKeepExactCellWidthsAndAdjacentPositions(t *testing.T) {
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Setenv("TERM", "xterm-256color")
	rows := []flatSpan{
		{span: flameSpan("one", "", "api", "界 long label", 100, 101)},
		{span: flameSpan("two", "", "api", "界 long label", 101, 103)},
		{span: flameSpan("three", "", "api", "界 long label", 103, 106)},
		{span: flameSpan("extent", "", "worker", "full extent", 100, 118), depth: 1},
	}
	for _, ascii := range []string{"0", "1"} {
		t.Setenv("SPANIEL_ASCII", ascii)
		for _, plain := range []string{"", "1"} {
			t.Setenv("NO_COLOR", plain)
			for selected := range rows[:3] {
				layout := layoutFlame(rows, 18)
				lines := renderFlame(rows, spanKey(rows[selected].span), 24, 6)
				line := ansi.Strip(lines[2]) // Title, ruler, then depth zero.
				for i, bar := range layout.lanes[0].bars {
					rendered := renderFlameBar(bar, i == selected)
					if got := ansi.StringWidth(rendered); got != i+1 {
						t.Fatalf("ASCII=%s plain=%s selected=%d: bar %d width=%d, want %d", ascii, plain, selected, i, got, i+1)
					}
					if got := ansi.Cut(line, 6+bar.left, 6+bar.right); got != ansi.Strip(rendered) {
						t.Fatalf("adjacent bar shifted from cells [%d,%d): got %q, want %q; row %q", bar.left, bar.right, got, ansi.Strip(rendered), line)
					}
				}
			}
		}
	}
}
