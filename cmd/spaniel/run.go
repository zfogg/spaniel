package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/storage"
)

// runSummary holds the per-session stats printed after the child exits.
type runSummary struct {
	TraceCount int
	SpanCount  int
	ErrorCount int
	N1Count    int
	SlowestURL string
}

// spawnRunOpts configures spawnRun's behaviour.
type spawnRunOpts struct {
	// apiBase is the spaniel API root, e.g. "http://localhost:8080".
	apiBase string
	// label is used as the session label and as service.name.
	label string
	// args is the child command.
	args []string
	// baseline: mark the session as baseline after the child exits.
	baseline bool
	// openBrowser: open the session diff page after the child exits.
	openBrowser bool
	// daemonCfg, when non-nil, starts an embedded spaniel daemon if none is
	// already reachable. Pass nil to require an already-running spaniel.
	daemonCfg *runConfig
	// out receives human-readable output (defaults to os.Stdout).
	out io.Writer
}

// spawnRun runs opts.args as a child process, injects OTLP env vars, waits
// for it to finish, and prints a summary. It returns the child's exit code.
func spawnRun(opts spawnRunOpts) (int, error) {
	if opts.out == nil {
		opts.out = os.Stdout
	}

	// 1. Ensure a spaniel instance is reachable.
	if err := ensureSpaniel(opts.apiBase, opts.daemonCfg); err != nil {
		return 1, fmt.Errorf("spaniel not available: %w", err)
	}

	// 2. Create + activate a fresh session for this run.
	sessID, err := createAndActivateSession(opts.apiBase, opts.label)
	if err != nil {
		return 1, fmt.Errorf("create session: %w", err)
	}

	// 3. Derive OTLP endpoint from apiBase (assume same host, port 4318).
	otlpEndpoint := otlpEndpointFrom(opts.apiBase)

	// 4. Build env: inherit current env, inject OTel vars.
	env := append(os.Environ(),
		"OTEL_EXPORTER_OTLP_ENDPOINT="+otlpEndpoint,
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES=service.name="+opts.label,
		"SPANIEL_SESSION_ID="+sessID,
	)

	// 5. Spawn child.
	exitCode, err := runChild(opts.args, env)
	if err != nil {
		// runChild only returns an error for setup failures; propagate the
		// exit code regardless so the caller can forward it.
		return exitCode, err
	}

	// 6. Brief pause so any in-flight spans can flush before we read stats.
	time.Sleep(300 * time.Millisecond)

	// 7. Print summary.
	summary, err := fetchSummary(opts.apiBase, sessID)
	if err == nil {
		printSummary(opts.out, opts.label, summary)
	}

	// 8. Optionally baseline the session (record subcommand).
	if opts.baseline {
		if berr := setBaseline(opts.apiBase, sessID); berr != nil {
			fmt.Fprintf(opts.out, "warning: could not mark baseline: %v\n", berr)
		} else {
			fmt.Fprintf(opts.out, "session marked as baseline\n")
		}
	}

	// 9. Optionally open browser.
	if opts.openBrowser {
		openBrowser(opts.apiBase + "/sessions")
	}

	return exitCode, nil
}

// ensureSpaniel checks whether spaniel is reachable at apiBase. If not and
// daemonCfg is non-nil, it starts an embedded daemon and waits up to 2s.
func ensureSpaniel(apiBase string, daemonCfg *runConfig) error {
	if healthOK(apiBase) {
		return nil
	}
	if daemonCfg == nil {
		return fmt.Errorf("no spaniel listening at %s (start it first, or use --api)", apiBase)
	}
	cfg := *daemonCfg
	cfg.NoBrowser = true
	go run(cfg) //nolint:errcheck
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if healthOK(apiBase) {
			return nil
		}
	}
	return fmt.Errorf("spaniel did not start within 2s at %s", apiBase)
}

// healthOK returns true if GET apiBase/api/health returns HTTP 200.
func healthOK(apiBase string) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(apiBase + "/api/health") //nolint:gosec
	if err != nil {
		return false
	}
	resp.Body.Close() //nolint:errcheck
	return resp.StatusCode == 200
}

// createAndActivateSession POSTs to /api/sessions and then activates it.
// Returns the new session ID.
func createAndActivateSession(apiBase, label string) (string, error) {
	body, _ := json.Marshal(map[string]string{"label": label})
	resp, err := http.Post(apiBase+"/api/sessions", "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST /api/sessions: HTTP %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	id := result.Data.ID
	if id == "" {
		return "", fmt.Errorf("empty session ID in response")
	}
	actResp, err := http.Post(apiBase+"/api/sessions/"+id+"/activate", "application/json", nil) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("activate session: %w", err)
	}
	actResp.Body.Close() //nolint:errcheck
	return id, nil
}

// runChild forks args[0] with args[1:], inheriting stdio. Returns the exit
// code. Errors are only returned for pre-exec failures.
func runChild(args []string, env []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("no command specified")
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// fetchSummary reads the full session list (which includes computed fields) and
// the trace list to build the run summary.
func fetchSummary(apiBase, sessID string) (runSummary, error) {
	var sum runSummary

	// ListSessions computes trace_count, error_count, n1_count server-side.
	sessions, err := fetchSessions(apiBase)
	if err != nil {
		return sum, err
	}
	for _, s := range sessions {
		if s.ID == sessID {
			sum.TraceCount = s.TraceCount
			sum.SpanCount = s.SpanCount
			sum.ErrorCount = s.ErrorCount
			sum.N1Count = s.N1Count
			break
		}
	}

	// Slowest trace: sort by duration desc, take first.
	tracesResp, err := http.Get(fmt.Sprintf("%s/api/traces?sessionId=%s&sort=duration_desc&limit=1", apiBase, sessID)) //nolint:gosec
	if err == nil {
		defer tracesResp.Body.Close() //nolint:errcheck
		var tracesEnv struct {
			Data []storage.TraceRow `json:"data"`
		}
		if json.NewDecoder(tracesResp.Body).Decode(&tracesEnv) == nil && len(tracesEnv.Data) > 0 {
			sum.SlowestURL = fmt.Sprintf("%s/traces/%s", apiBase, tracesEnv.Data[0].TraceID)
		}
	}

	return sum, nil
}

// setBaseline marks a session as a baseline via POST /api/sessions/:id/baseline.
func setBaseline(apiBase, sessID string) error {
	body, _ := json.Marshal(map[string]bool{"is_baseline": true})
	resp, err := http.Post(apiBase+"/api/sessions/"+sessID+"/baseline", "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return err
	}
	resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// printSummary writes a one-line summary to w.
func printSummary(w io.Writer, label string, s runSummary) {
	parts := []string{
		fmt.Sprintf("%d traces", s.TraceCount),
		fmt.Sprintf("%d spans", s.SpanCount),
	}
	if s.ErrorCount > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", s.ErrorCount))
	}
	if s.N1Count > 0 {
		parts = append(parts, fmt.Sprintf("%d N+1", s.N1Count))
	}
	fmt.Fprintf(w, "%s: %s\n", label, strings.Join(parts, " · "))
	if s.SlowestURL != "" {
		fmt.Fprintf(w, "slowest trace: %s\n", s.SlowestURL)
	}
}

// otlpEndpointFrom replaces the port in apiBase with 4318 (default OTLP HTTP).
// e.g. "http://localhost:8080" → "http://localhost:4318"
func otlpEndpointFrom(apiBase string) string {
	// Simple string replacement: find the last colon+port and swap it.
	if i := strings.LastIndex(apiBase, ":"); i >= 0 {
		// Make sure it's actually a port (i.e. after the host, not in http://).
		rest := apiBase[i+1:]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			return apiBase[:i] + ":4318"
		}
	}
	return apiBase + ":4318"
}

// cmdLabel returns a short label for a command: basename of the executable,
// or the first arg if the command is a shell one-liner.
func cmdLabel(args []string) string {
	if len(args) == 0 {
		return "run"
	}
	return filepath.Base(args[0])
}

// ── cobra commands ───────────────────────────────────────────────────────────

func runSubcommand() *cobra.Command {
	var apiBase string
	cmd := &cobra.Command{
		Use:   "run -- <cmd> [args...]",
		Short: "Run a command with OTLP auto-instrumentation, print a trace summary",
		Long: `Spawns <cmd> with OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_RESOURCE_ATTRIBUTES
injected so any OpenTelemetry-instrumented binary sends traces to spaniel
automatically. A fresh session is created for the run; stats are printed
when the command exits. spaniel is started automatically if it's not already
running.`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			label := cmdLabel(args)
			code, err := spawnRun(spawnRunOpts{
				apiBase: apiBase,
				label:   label,
				args:    args,
			})
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	return cmd
}

func recordSubcommand() *cobra.Command {
	var apiBase string
	var noBrowserFlag bool
	cmd := &cobra.Command{
		Use:   "record -- <cmd> [args...]",
		Short: "Run a command, store traces as a baseline session, open browser diff",
		Long: `Like 'spaniel run' but also marks the session as a baseline and opens
the browser to the session diff page when the command finishes.`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			label := cmdLabel(args)
			code, err := spawnRun(spawnRunOpts{
				apiBase:     apiBase,
				label:       label,
				args:        args,
				baseline:    true,
				openBrowser: !noBrowserFlag,
			})
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.Flags().BoolVar(&noBrowserFlag, "no-browser", false, "Do not open browser after recording")
	return cmd
}
