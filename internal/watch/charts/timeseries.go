// Package charts is the shared Charm v1 chart adapter for metrics and panels.
package charts

import (
	tslc "github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
	"math"
	"sort"
	"time"
)

type Point struct {
	Time  time.Time
	Value float64
}
type Series struct {
	Name   string
	Points []Point
}

func NewTimeSeries(width, height int, from, to time.Time, minY, maxY float64) tslc.Model {
	if !to.After(from) {
		to = from.Add(time.Second)
	}
	if maxY <= minY {
		maxY = minY + 1
	}
	return tslc.New(max(8, width), max(4, height), tslc.WithTimeRange(from, to), tslc.WithYRange(minY, maxY), tslc.WithAxesStyles(lipgloss.NewStyle().Foreground(lipgloss.Color("8")), lipgloss.NewStyle().Foreground(lipgloss.Color("7"))))
}

// RenderTimeSeries sorts a private copy, replaces duplicate timestamps and
// bounds each dataset. It does no aggregation or unit conversion.
func RenderTimeSeries(width, height int, from, to time.Time, series []Series) string {
	minY, maxY := math.Inf(1), math.Inf(-1)
	sets := make([]Series, 0, len(series))
	for _, s := range series {
		byTime := map[time.Time]float64{}
		for _, p := range s.Points {
			if !math.IsNaN(p.Value) && !math.IsInf(p.Value, 0) {
				byTime[p.Time] = p.Value
			}
		}
		points := make([]Point, 0, len(byTime))
		for t, v := range byTime {
			points = append(points, Point{t, v})
		}
		sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
		if len(points) > 2048 {
			points = points[len(points)-2048:]
		}
		for _, p := range points {
			minY = math.Min(minY, p.Value)
			maxY = math.Max(maxY, p.Value)
		}
		sets = append(sets, Series{s.Name, points})
	}
	if math.IsInf(minY, 1) {
		return "No samples in this time window."
	}
	chart := NewTimeSeries(width, height, from, to, minY, maxY)
	names := make([]string, 0, len(sets))
	for _, s := range sets {
		names = append(names, s.Name)
		for _, p := range s.Points {
			chart.PushDataSet(s.Name, tslc.TimePoint{Time: p.Time, Value: p.Value})
		}
	}
	chart.DrawBrailleDataSets(names)
	return chart.View()
}
