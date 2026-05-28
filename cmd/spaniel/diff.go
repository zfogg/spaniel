package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/storage"
)

// diffSubcommand is `spaniel diff` — compares two sessions via /api/diff and
// either launches the TUI (interactive) or emits machine-readable JSON.
// The exit code gate (threshold + spans_added > 0) fires in both modes so
// CI pipelines using --json keep working.
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
		Long: `Compare two sessions and either open an interactive pager or
emit JSON for CI.

Both --baseline and --compare accept either a session id or its label.
With --json the full DiffResult is printed verbatim for piping into jq.
Without --json the result is shown in an interactive TUI (requires a TTY).
Exits 1 when duration_delta_pct > --threshold OR spans_added > 0.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseline == "" || compare == "" {
				return fmt.Errorf("both --baseline and --compare are required")
			}
			if !asJSON && !isTerminal(os.Stdout) {
				return fmt.Errorf("non-TTY output requires --json (interactive mode needs a terminal)")
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

// runDiff orchestrates: resolve refs → fetch → render (TUI or JSON) → gate.
// `out` is honored for the JSON path and ignored for the TUI path (which
// owns its own renderer). The exit-code gate fires via errDiffRegressed in
// both modes so callers (CI and humans) get the same signal.
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
		if err := diffTUI(result); err != nil {
			return err
		}
	}

	if diffFailsGate(result, threshold) {
		return errDiffRegressed
	}
	return nil
}

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
// and matching either field. Exact-id match wins over label match.
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

// ── shared helpers (used by both --json result inspection and the TUI) ─────

func sortRegressedDesc(spans []api.DiffSpan) []api.DiffSpan {
	var out []api.DiffSpan
	for _, s := range spans {
		if s.Status == "changed" && s.DeltaPct > 0 {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeltaPct > out[j].DeltaPct })
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

