package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// traceSpan is a subset of storage.Span — only the fields the renderer
// needs. Decoupling lets the CLI build without importing the storage
// package (and its DuckDB cgo deps when cross-compiling).
type traceSpan struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	ServiceName  string `json:"service_name"`
	Name         string `json:"name"`
	StartNs      int64  `json:"start_ns"`
	EndNs        int64  `json:"end_ns"`
	DurationNs   int64  `json:"duration_ns"`
	StatusCode   int    `json:"status_code"`
	Tag          string `json:"tag,omitempty"`
}

// traceSubcommand returns `spaniel trace <id>` — an interactive Bubble Tea
// waterfall. Requires a TTY; the old plain-text print path was removed.
func traceSubcommand() *cobra.Command {
	var apiBase string
	cmd := &cobra.Command{
		Use:   "trace <id|short-id>",
		Short: "Open an interactive trace waterfall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal(os.Stdout) {
				return fmt.Errorf("spaniel trace requires a terminal — run it interactively")
			}
			spans, err := fetchTrace(apiBase, args[0])
			if err != nil {
				return err
			}
			if len(spans) == 0 {
				return fmt.Errorf("no spans found for trace %q", args[0])
			}
			return traceTUI(spans)
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.SilenceUsage = true
	return cmd
}

// fetchTrace tries an exact-id lookup first, then falls back to listing
// traces and matching by short-id prefix.
func fetchTrace(apiBase, idOrPrefix string) ([]*traceSpan, error) {
	spans, err := getTrace(apiBase, idOrPrefix)
	if err == nil && len(spans) > 0 {
		return spans, nil
	}
	full, lookupErr := resolveTraceID(apiBase, idOrPrefix)
	if lookupErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, lookupErr
	}
	return getTrace(apiBase, full)
}

func getTrace(apiBase, id string) ([]*traceSpan, error) {
	resp, err := http.Get(apiBase + "/api/traces/" + id)
	if err != nil {
		return nil, fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch trace %s: %d %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var env struct {
		Data []*traceSpan `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode trace: %w", err)
	}
	return env.Data, nil
}

func resolveTraceID(apiBase, prefix string) (string, error) {
	resp, err := http.Get(apiBase + "/api/traces?limit=500")
	if err != nil {
		return "", fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list traces: HTTP %d", resp.StatusCode)
	}
	var env struct {
		Data []struct {
			TraceID string `json:"trace_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return "", fmt.Errorf("decode trace list: %w", err)
	}
	var matches []string
	for _, t := range env.Data {
		if strings.HasPrefix(t.TraceID, prefix) {
			matches = append(matches, t.TraceID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no trace matches prefix %q", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous trace prefix %q (%d matches)", prefix, len(matches))
	}
}

// sortSpansByStart returns a copy of spans ordered by StartNs ascending —
// the order the waterfall reads top-down.
func sortSpansByStart(spans []*traceSpan) []*traceSpan {
	sorted := append([]*traceSpan(nil), spans...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartNs < sorted[j].StartNs })
	return sorted
}

// traceWindow returns the [min start, max end] of the slice. Used both by
// the TUI for bar scaling and by the printf summary.
func traceWindow(spans []*traceSpan) (start, end int64) {
	start, end = spans[0].StartNs, spans[0].EndNs
	for _, s := range spans {
		if s.StartNs < start {
			start = s.StartNs
		}
		if s.EndNs > end {
			end = s.EndNs
		}
	}
	return
}

// computeDepths walks parent links to compute the indent depth of each span.
// Orphans (parent not present in the slice) are treated as roots at depth 0.
func computeDepths(spans []*traceSpan) map[string]int {
	byID := make(map[string]*traceSpan, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = s
	}
	depth := make(map[string]int, len(spans))
	var walk func(id string) int
	walk = func(id string) int {
		if d, ok := depth[id]; ok {
			return d
		}
		s, ok := byID[id]
		if !ok || s.ParentSpanID == "" {
			depth[id] = 0
			return 0
		}
		if _, parentExists := byID[s.ParentSpanID]; !parentExists {
			depth[id] = 0
			return 0
		}
		d := walk(s.ParentSpanID) + 1
		depth[id] = d
		return d
	}
	for _, s := range spans {
		walk(s.SpanID)
	}
	return depth
}

func humanNs(ns int64) string {
	switch {
	case ns >= 1_000_000_000:
		return fmt.Sprintf("%.2fs", float64(ns)/1e9)
	case ns >= 1_000_000:
		return fmt.Sprintf("%dms", ns/1_000_000)
	case ns >= 1_000:
		return fmt.Sprintf("%dµs", ns/1_000)
	default:
		return fmt.Sprintf("%dns", ns)
	}
}

func tagLabel(t string) string {
	switch t {
	case "n_plus_one", "n+1":
		return "[N+1]"
	case "error":
		return "[ERROR]"
	case "slow":
		return "[SLOW]"
	case "lint":
		return "[LINT]"
	default:
		return "[" + strings.ToUpper(t) + "]"
	}
}

// isTerminal returns true if the given file is a TTY. A minimal stat-based
// check that doesn't pull in golang.org/x/term: character devices are TTYs.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

