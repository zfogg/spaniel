package traces

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/watch/client"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

func TestInspectorsCollapseHealthyStatusAndKeepMeaningfulBanners(t *testing.T) {
	span := sampleSpan("span", "", 1793631000000000000)
	trace := NewDetails().Trace(testEnv("trace-detail"), protocol.OpenTrace{TraceID: "trace"}).(*traceModel)
	trace.Update(result(trace.env.Owner, "trace", 1, client.Envelope[[]apigen.Span]{Data: []apigen.Span{span}}))
	detail := NewDetails().Span(testEnv("span-detail"), protocol.OpenSpan{TraceID: "trace", SpanID: "span"}).(*spanModel)
	detail.Update(result(detail.env.Owner, "span", 1, client.Envelope[apigen.Span]{Data: span}))
	for _, tc := range []struct {
		name  string
		state *viewState
		view  func() string
	}{{"waterfall", &trace.state, trace.View}, {"span", &detail.state, detail.View}} {
		t.Run(tc.name, func(t *testing.T) {
			tc.state.connection.Connected = true
			lines := strings.Split(ansi.Strip(tc.view()), "\n")
			if !strings.HasPrefix(lines[1], "FIELD") || strings.Contains(strings.Join(lines, "\n"), "\nLIVE\n") {
				t.Fatal("healthy status reserved a row above metadata", lines)
			}
			for _, state := range []struct {
				loading, pending, connected bool
				err, want                   string
			}{{true, false, true, "", "Loading"}, {false, true, true, "", "Syncing"}, {false, false, false, "", "disconnected"}, {false, false, true, "lookup failed", "lookup failed"}} {
				tc.state.loading, tc.state.pending, tc.state.connection.Connected, tc.state.err = state.loading, state.pending, state.connected, state.err
				lines = strings.Split(ansi.Strip(tc.view()), "\n")
				if !strings.Contains(lines[1], state.want) || !strings.HasPrefix(lines[2], "FIELD") {
					t.Fatal("meaningful status lost or geometry shifted", state.want, lines)
				}
			}
		})
	}
	detail.state = viewState{connection: protocol.Connection{Connected: true}}
	detail.height = 6
	lines := strings.Split(ansi.Strip(detail.View()), "\n")
	if len(lines) != 6 || !strings.Contains(lines[5], "Status") {
		t.Fatal("healthy inspector did not reclaim status row space", lines)
	}
}
