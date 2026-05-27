package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/ci"
)

// ciSubcommand assembles the `spaniel ci` group: `export` writes a baseline
// snapshot to disk, `check` compares the running session against a baseline
// file and exits non-zero on any regression. Both are non-interactive and
// emit JSON for easy parsing in GitHub Actions.
func ciSubcommand() *cobra.Command {
	var apiBase string

	ciCmd := &cobra.Command{
		Use:   "ci",
		Short: "CI helpers: export a baseline snapshot, check for regressions",
	}
	ciCmd.PersistentFlags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")

	var (
		exportOutput  string
		exportSession string
	)
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export the active (or named) session as a baseline JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ciExport(apiBase, exportSession, exportOutput)
		},
	}
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "spaniel-baseline.json", "Path to write the baseline JSON")
	exportCmd.Flags().StringVar(&exportSession, "session", "", "Session ID to export (default: active session)")
	ciCmd.AddCommand(exportCmd)

	var (
		checkBaseline   string
		checkSession    string
		checkThreshold  float64
		checkN1         int
		checkJSON       bool
	)
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Compare the active session against a baseline file; exit 1 on regression",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ciCheck(apiBase, checkBaseline, checkSession, ci.CheckOptions{
				DurationThresholdPct: checkThreshold,
				N1Threshold:          checkN1,
			}, checkJSON)
		},
	}
	checkCmd.Flags().StringVarP(&checkBaseline, "baseline", "b", "spaniel-baseline.json", "Baseline JSON to compare against")
	checkCmd.Flags().StringVar(&checkSession, "session", "", "Session ID to check (default: active session)")
	checkCmd.Flags().Float64Var(&checkThreshold, "threshold", 20, "p95 / root-duration regression threshold (percent)")
	checkCmd.Flags().IntVar(&checkN1, "n1-threshold", 10, "Extra repetitions of a fingerprint that count as an N+1 regression")
	checkCmd.Flags().BoolVar(&checkJSON, "json", false, "Emit machine-readable JSON instead of a human summary")
	ciCmd.AddCommand(checkCmd)

	return ciCmd
}

// ciExport fetches a snapshot from a running spaniel and writes it to disk.
func ciExport(apiBase, sessionID, output string) error {
	if sessionID == "" {
		var err error
		sessionID, err = activeSessionID(apiBase)
		if err != nil {
			return err
		}
	}
	snap, err := fetchSnapshot(apiBase, sessionID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", output, err)
	}
	fmt.Printf("exported baseline: %s (%d operations, %d N+1, %d errors)\n",
		output, len(snap.Operations), len(snap.NPlusOne), len(snap.Errors))
	return nil
}

// ciCheck loads a baseline file, fetches the current snapshot, compares them,
// and exits non-zero if regressions are found.
func ciCheck(apiBase, baselinePath, sessionID string, opts ci.CheckOptions, asJSON bool) error {
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		return fmt.Errorf("read baseline %s: %w", baselinePath, err)
	}
	var baseline ci.Snapshot
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return fmt.Errorf("parse baseline: %w", err)
	}
	if sessionID == "" {
		sessionID, err = activeSessionID(apiBase)
		if err != nil {
			return err
		}
	}
	current, err := fetchSnapshot(apiBase, sessionID)
	if err != nil {
		return err
	}

	res := ci.Compare(baseline, current, opts)

	if asJSON {
		out, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(out))
	} else {
		printCheckSummary(res)
	}
	if !res.Pass {
		os.Exit(1)
	}
	return nil
}

func printCheckSummary(res ci.Result) {
	if res.Pass {
		fmt.Println("✓ no regressions detected")
		return
	}
	fmt.Printf("✗ %d regression(s) detected:\n", len(res.Regressions))
	for _, r := range res.Regressions {
		fmt.Printf("  [%s] %s\n", r.Kind, r.Message)
	}
}

func activeSessionID(apiBase string) (string, error) {
	resp, err := http.Get(apiBase + "/api/sessions/active") //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("active session lookup: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data struct{ ID string } `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Data.ID == "" {
		return "", fmt.Errorf("no active session — pass --session or start one with `spaniel session new`")
	}
	return body.Data.ID, nil
}

func fetchSnapshot(apiBase, sessionID string) (ci.Snapshot, error) {
	resp, err := http.Get(apiBase + "/api/sessions/" + sessionID + "/baseline-export") //nolint:gosec
	if err != nil {
		return ci.Snapshot{}, fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return ci.Snapshot{}, fmt.Errorf("baseline-export: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var body struct {
		Data ci.Snapshot `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ci.Snapshot{}, err
	}
	return body.Data, nil
}
