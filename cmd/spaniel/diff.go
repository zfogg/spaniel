package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/storage"
)

// diffSubcommand is the top-level `spaniel diff` command. It diffs two
// sessions (by id or label) via the running spaniel's /api/diff endpoint
// and prints a human summary — or pretty JSON when --json is set. Exits
// non-zero when duration_delta_pct exceeds --threshold or any spans were
// added (the CI gate from issue #49).
func diffSubcommand() *cobra.Command {
	var (
		apiBase   string
		baseline  string
		compare   string
		threshold float64
		asJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff two sessions (id or label) via /api/diff",
		Long: `Compare two sessions and print a regression summary.

Both --baseline and --compare accept either a session id or its label.
With --json the full DiffResult is printed verbatim for piping into jq.
Exits 1 when duration_delta_pct > --threshold OR spans_added > 0 so the
command slots into CI gates.`,
		// Suppress cobra's auto-usage / auto-error print when the gate trips —
		// the summary is the user-facing signal; the non-zero exit is for CI.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseline == "" || compare == "" {
				return fmt.Errorf("both --baseline and --compare are required")
			}
			err := runDiff(apiBase, baseline, compare, threshold, asJSON, cmd.OutOrStdout())
			if err == errDiffRegressed {
				os.Exit(1)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.Flags().StringVarP(&baseline, "baseline", "b", "", "Baseline session id or label")
	cmd.Flags().StringVarP(&compare, "compare", "c", "", "Compare session id or label")
	cmd.Flags().Float64Var(&threshold, "threshold", 10, "Fail if duration_delta_pct exceeds this percentage")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the full DiffResult as JSON")
	return cmd
}

// runDiff orchestrates the resolve → fetch → render → gate pipeline.
// Output is written to `out` so tests can capture it; the exit-code gate
// only fires when invoked from a real process (os.Exit is harness-unsafe
// in tests — runDiff returns a sentinel error instead).
func runDiff(apiBase, baselineRef, compareRef string, threshold float64, asJSON bool, out io.Writer) error {
	baselineID, err := resolveSessionRef(apiBase, baselineRef)
	if err != nil {
		return fmt.Errorf("resolve --baseline %q: %w", baselineRef, err)
	}
	compareID, err := resolveSessionRef(apiBase, compareRef)
	if err != nil {
		return fmt.Errorf("resolve --compare %q: %w", compareRef, err)
	}

	result, err := fetchDiff(apiBase, baselineID, compareID)
	if err != nil {
		return err
	}

	if asJSON {
		raw, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(out, string(raw))
	} else {
		printDiffSummary(out, result)
	}

	if diffFailsGate(result, threshold) {
		return errDiffRegressed
	}
	return nil
}

// errDiffRegressed is returned by runDiff when the threshold or
// spans_added gate trips. main.go's diff command converts this into a
// non-zero exit code without printing the error a second time.
var errDiffRegressed = fmt.Errorf("regression detected")

func diffFailsGate(r api.DiffResult, threshold float64) bool {
	if r.Summary.DurationDeltaPct > threshold {
		return true
	}
	if r.Summary.SpansAdded > 0 {
		return true
	}
	return false
}

// resolveSessionRef resolves a "ref" (id or label) by GETting /api/sessions
// and matching either field. Exact-id match wins over label match, so
// labels that happen to look like ids still pick the right session.
func resolveSessionRef(apiBase, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty session ref")
	}
	resp, err := http.Get(apiBase + "/api/sessions") //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("list sessions: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data []storage.Session `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("parse sessions: %w", err)
	}
	for _, s := range envelope.Data {
		if s.ID == ref {
			return s.ID, nil
		}
	}
	for _, s := range envelope.Data {
		if s.Label == ref {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no session with id or label %q", ref)
}

func fetchDiff(apiBase, baselineID, compareID string) (api.DiffResult, error) {
	q := url.Values{}
	q.Set("baseline", baselineID)
	q.Set("compare", compareID)
	resp, err := http.Get(apiBase + "/api/diff?" + q.Encode()) //nolint:gosec
	if err != nil {
		return api.DiffResult{}, fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return api.DiffResult{}, fmt.Errorf("/api/diff: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data api.DiffResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return api.DiffResult{}, fmt.Errorf("parse diff: %w", err)
	}
	return envelope.Data, nil
}

// ── human summary ──────────────────────────────────────────────────────────

func printDiffSummary(out io.Writer, r api.DiffResult) {
	bw := newColWriter(out)

	fmt.Fprintf(bw, "baseline:\t%s\t(%s · %d spans · %s)\n",
		r.Baseline.Label, shortID(r.Baseline.SessionID), r.Baseline.SpanCount, fmtNs(r.Baseline.TotalDurationNs))
	fmt.Fprintf(bw, "compare:\t%s\t(%s · %d spans · %s)\n",
		r.Compare.Label, shortID(r.Compare.SessionID), r.Compare.SpanCount, fmtNs(r.Compare.TotalDurationNs))
	bw.flush()

	fmt.Fprintln(out)
	fmt.Fprintln(out, "summary:")
	sw := newColWriter(out)
	fmt.Fprintf(sw, "  duration\t%s → %s\t(%s)\n",
		fmtNs(r.Baseline.TotalDurationNs), fmtNs(r.Compare.TotalDurationNs), pctStr(r.Summary.DurationDeltaPct))
	fmt.Fprintf(sw, "  spans\t%d → %d\t(%s)\n",
		r.Baseline.SpanCount, r.Compare.SpanCount, deltaStr(r.Compare.SpanCount-r.Baseline.SpanCount))
	fmt.Fprintf(sw, "  db calls\t%d → %d\t(%s)\n",
		r.Baseline.DBCalls, r.Compare.DBCalls, deltaStr(r.Summary.DBCallDelta))
	sw.flush()

	// regressed = changed spans with positive DeltaPct, sorted desc.
	var regressed []api.DiffSpan
	for _, s := range r.Spans {
		if s.Status == "changed" && s.DeltaPct > 0 {
			regressed = append(regressed, s)
		}
	}
	sort.Slice(regressed, func(i, j int) bool { return regressed[i].DeltaPct > regressed[j].DeltaPct })
	if len(regressed) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "regressed:")
		rw := newColWriter(out)
		for _, s := range regressed {
			fmt.Fprintf(rw, "  %s · %s\t%s → %s\t(%s)\n",
				s.ServiceName, s.Name,
				fmtNs(s.BaselineDurationNs), fmtNs(s.CompareDurationNs),
				pctStr(s.DeltaPct))
		}
		rw.flush()
	}

	addedNames   := pickNames(r.Spans, "added")
	removedNames := pickNames(r.Spans, "removed")
	if len(addedNames) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "added: %d (%s)\n", len(addedNames), strings.Join(addedNames, ", "))
	}
	if len(removedNames) > 0 {
		fmt.Fprintf(out, "removed: %d (%s)\n", len(removedNames), strings.Join(removedNames, ", "))
	}
}

func pickNames(spans []api.DiffSpan, status string) []string {
	var out []string
	for _, s := range spans {
		if s.Status == status {
			out = append(out, s.Name)
		}
	}
	return out
}

func shortID(id string) string {
	if len(id) <= 9 {
		return id
	}
	return id[:4] + "…" + id[len(id)-5:]
}

func pctStr(pct float64) string {
	sign := "+"
	if pct < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, pct)
}

func deltaStr(n int) string {
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprintf("%d", n)
}

// fmtNs mirrors the frontend formatter so CLI output matches the UI.
func fmtNs(ns int64) string {
	if ns < 1_000 {
		return fmt.Sprintf("%dns", ns)
	}
	if ns < 1_000_000 {
		return fmt.Sprintf("%.1fµs", float64(ns)/1_000)
	}
	if ns < 1_000_000_000 {
		return fmt.Sprintf("%.1fms", float64(ns)/1_000_000)
	}
	return fmt.Sprintf("%.2fs", float64(ns)/1_000_000_000)
}

// colWriter is a tiny tab-aligned writer so we don't pull in text/tabwriter
// for one-off summary tables. Columns are separated by literal '\t' chars.
type colWriter struct {
	w     io.Writer
	rows  [][]string
	cols  []int
}

func newColWriter(w io.Writer) *colWriter { return &colWriter{w: w} }

func (c *colWriter) Write(p []byte) (int, error) {
	// Strip trailing newline, split into cells, accumulate.
	line := strings.TrimSuffix(string(p), "\n")
	cells := strings.Split(line, "\t")
	for i, cell := range cells {
		if i >= len(c.cols) {
			c.cols = append(c.cols, 0)
		}
		if w := len(cell); w > c.cols[i] {
			c.cols[i] = w
		}
	}
	c.rows = append(c.rows, cells)
	return len(p), nil
}

func (c *colWriter) flush() {
	for _, row := range c.rows {
		for i, cell := range row {
			if i < len(row)-1 {
				fmt.Fprintf(c.w, "%-*s  ", c.cols[i], cell)
			} else {
				fmt.Fprintln(c.w, cell)
			}
		}
	}
	c.rows = nil
	c.cols = nil
}

