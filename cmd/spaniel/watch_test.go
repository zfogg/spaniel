package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunWatch_PrintsSpanLines(t *testing.T) {
	frames := []map[string]any{
		{"type": "span", "payload": map[string]any{
			"serviceName": "checkout-api", "name": "GET /cart", "durationNs": 42_000_000, "statusCode": 1, "traceId": "trace-a",
		}},
		{"type": "span", "payload": map[string]any{
			"serviceName": "checkout-api", "name": "POST /pay", "durationNs": 105_000_000, "statusCode": 2, "traceId": "trace-b",
		}},
	}
	srv, wg := newWSPusher(t, frames)
	defer srv.Close()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = runWatch(ctx, &buf, srv.URL, spanFilter{})
	}()
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	out := buf.String()
	for _, want := range []string{"GET /cart", "POST /pay", "42ms", "105ms", "error"} {
		if !strings.Contains(out, want) {
			t.Errorf("watch output missing %q\n%s", want, out)
		}
	}
}

func TestRunWatch_ErrorsOnlyFiltersOK(t *testing.T) {
	frames := []map[string]any{
		{"type": "span", "payload": map[string]any{"serviceName": "api", "name": "good", "durationNs": 1_000_000, "statusCode": 1}},
		{"type": "span", "payload": map[string]any{"serviceName": "api", "name": "bad", "durationNs": 1_000_000, "statusCode": 2}},
	}
	srv, wg := newWSPusher(t, frames)
	defer srv.Close()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = runWatch(ctx, &buf, srv.URL, spanFilter{ErrorsOnly: true})
	}()
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	out := buf.String()
	if strings.Contains(out, "good") {
		t.Errorf("ok span should be filtered with --errors-only:\n%s", out)
	}
	if !strings.Contains(out, "bad") {
		t.Errorf("error span should pass --errors-only:\n%s", out)
	}
}

func TestRunWatch_ServiceFilter(t *testing.T) {
	frames := []map[string]any{
		{"type": "span", "payload": map[string]any{"serviceName": "wanted", "name": "yes", "durationNs": 1_000_000, "statusCode": 1}},
		{"type": "span", "payload": map[string]any{"serviceName": "other", "name": "no", "durationNs": 1_000_000, "statusCode": 1}},
	}
	srv, wg := newWSPusher(t, frames)
	defer srv.Close()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = runWatch(ctx, &buf, srv.URL, spanFilter{Service: "wanted"})
	}()
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	out := buf.String()
	if !strings.Contains(out, "yes") {
		t.Errorf("wanted service line missing:\n%s", out)
	}
	if strings.Contains(out, " no ") || strings.Contains(out, " no\n") {
		t.Errorf("other service line should be filtered:\n%s", out)
	}
}

func TestRunWatch_N1OnlyDropsRegularSpansShowsIssues(t *testing.T) {
	frames := []map[string]any{
		{"type": "span", "payload": map[string]any{"serviceName": "api", "name": "regular-span", "durationNs": 1_000_000, "statusCode": 1}},
		{"type": "issue", "payload": map[string]any{"traceId": "abcd1234efgh", "kind": "n_plus_one", "count": 12, "wastedNs": 50_000_000}},
	}
	srv, wg := newWSPusher(t, frames)
	defer srv.Close()

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = runWatch(ctx, &buf, srv.URL, spanFilter{N1Only: true})
	}()
	wg.Wait()
	time.Sleep(150 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	out := buf.String()
	if strings.Contains(out, "regular-span") {
		t.Errorf("regular spans should not appear with --n+1-only:\n%s", out)
	}
	if !strings.Contains(out, "N+1") {
		t.Errorf("expected N+1 issue line:\n%s", out)
	}
	if !strings.Contains(out, "count=12") {
		t.Errorf("expected count=12 in issue line:\n%s", out)
	}
}

func TestFormatSpan_TruncatesLongNames(t *testing.T) {
	s := &spanEntry{ServiceName: "svc", Name: strings.Repeat("x", 80), DurationNs: 1_000_000, StatusCode: 1}
	if !strings.Contains(formatSpan(s), "…") {
		t.Errorf("expected truncation ellipsis for 80-char name: %q", formatSpan(s))
	}
}
