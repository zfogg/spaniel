package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zfogg/spaniel/internal/storage"
)

func TestRenderWaterfallTiming(t *testing.T) {
	const base int64 = 1_793_672_049_959_832_600
	spans := []*storage.Span{
		{SpanID: "root", Name: "request", ServiceName: "api", StartNs: base, EndNs: base + 100_000_000, DurationNs: 100_000_000},
		{SpanID: "a", ParentSpanID: "root", Name: "first", StartNs: base + 10_000_000, EndNs: base + 60_000_000, DurationNs: 50_000_000},
		{SpanID: "b", ParentSpanID: "root", Name: "overlap", StartNs: base + 40_000_000, EndNs: base + 60_000_000, DurationNs: 20_000_000},
		// The final child outlives the root: scale must include it.
		{SpanID: "c", ParentSpanID: "root", Name: "late", StartNs: base + 150_000_000, EndNs: base + 200_000_000, DurationNs: 50_000_000},
	}
	out := renderWaterfall("trace", spans)
	for _, want := range []string{
		"4 spans, 200.000ms; start_unix_ns=1793672049959832600",
		"span_id=c",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("expected header and four rows:\n%s", out)
	}
	for i, want := range []string{
		"|####################....................| +0.000ms 100.000ms request",
		"|..##########............................| +10.000ms 50.000ms first",
		"|........####............................| +40.000ms 20.000ms overlap",
		"|..............................##########| +150.000ms 50.000ms late",
	} {
		got := strings.Join(strings.Fields(lines[i+2])[:4], " ")
		if got != want {
			t.Errorf("row %d: %s, want %s", i, got, want)
		}
	}
	// Duration/offset columns are fixed width so tree indentation is stable.
	if strings.Index(lines[3], "first") != strings.Index(lines[2], "request")+2 {
		t.Errorf("child indentation lost:\n%s", out)
	}
}

func TestRenderWaterfallFullNamesAndStatusWithoutAttributes(t *testing.T) {
	name := "storage.postgres." + strings.Repeat("long_operation_犬", 20) + "\nusers"
	message := "failed\r\ntry again " + strings.Repeat("犬", 100)
	span := &storage.Span{
		SpanID: "sql", Name: name, ServiceName: "db\r\nservice",
		StartNs: 1, EndNs: 12_346, DurationNs: 12_345, StatusCode: 2,
		StatusMessage: message,
		Attributes:    `{"db.query.text":"SELECT\n  *\tFROM users","exception.message":"` + strings.Repeat("犬", 100) + `"}`,
	}
	out := renderWaterfall("trace", []*storage.Span{span})
	if !utf8.ValidString(out) || len(strings.Split(out, "\n")) != 3 {
		t.Fatalf("expected valid UTF-8 and one span row:\n%s", out)
	}
	for _, want := range []string{"0.012ms", oneLine(name) + "  [db service]", "msg=" + oneLine(message)} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"db.query.text", "exception.message", "SELECT", "…"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in waterfall:\n%s", unwanted, out)
		}
	}
	if got := nsToMs(span.DurationNs); got != 0.012345 {
		t.Errorf("duration lost precision: %v", got)
	}
}

func TestRenderWaterfallIncompleteAndCyclic(t *testing.T) {
	spans := []*storage.Span{
		{SpanID: "orphan", ParentSpanID: "absent", Name: "orphan"},
		{SpanID: "zero-root", ParentSpanID: "0000000000000000", Name: "zero root"},
		{SpanID: "a", ParentSpanID: "b", Name: "cycle a"},
		{SpanID: "b", ParentSpanID: "a", Name: "cycle b"},
		{SpanID: "self", ParentSpanID: "self", Name: "self cycle"},
	}
	out := renderWaterfall("trace", spans)
	for _, span := range spans {
		n := 0
		for _, field := range strings.Fields(out) {
			if field == "span_id="+span.SpanID {
				n++
			}
		}
		if n != 1 {
			t.Errorf("span %s rendered %d times", span.SpanID, n)
		}
	}
	if !strings.Contains(out, "missing_parent=absent") || !strings.Contains(out, "invalid parent cycle") {
		t.Fatalf("missing incomplete/cyclic trace markers:\n%s", out)
	}
	if got := renderWaterfall("empty", nil); got != "trace empty: no spans found" {
		t.Errorf("empty trace: %s", got)
	}
}
