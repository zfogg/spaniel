package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/client"
)

// The standalone command uses the same live waterfall, span inspector,
// contextual footer and transport as watch, with one traces tab.
func traceSubcommand() *cobra.Command {
	var apiBase, session string
	cmd := &cobra.Command{Use: "trace <id|short-id>", Short: "Open a live interactive trace waterfall", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !isTerminal(os.Stdout) {
			return fmt.Errorf("spaniel trace requires a terminal — run it interactively")
		}
		api, err := client.New(apiBase, client.Options{})
		if err != nil {
			return err
		}
		id, scope, err := resolveTrace(cmd.Context(), api, session, args[0])
		if err != nil {
			return err
		}
		return watch.RunTrace(cmd.Context(), api, id, scope)
	}}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.Flags().StringVar(&session, "session", "", "Pin the trace to a recording session")
	cmd.SilenceUsage = true
	return cmd
}

// Preserve exact-ID lookup before the existing recent-list prefix lookup.
// The selected row's recording session then stays pinned for all live details.
func resolveTrace(ctx context.Context, api *client.Client, session, id string) (string, string, error) {
	spans, exactErr := api.Trace(ctx, id, client.Scope(nil, session))
	if exactErr == nil && len(spans.Data) > 0 {
		return spans.Data[0].TraceId, spans.Data[0].SessionId, nil
	}
	rows, err := api.Traces(ctx, client.Scope(url.Values{"limit": {"500"}}, session))
	if err != nil {
		if exactErr != nil {
			return "", "", exactErr
		}
		return "", "", err
	}
	foundID, foundSession, count := "", "", 0
	for _, row := range rows.Data {
		if strings.HasPrefix(row.TraceId, id) {
			foundID, foundSession = row.TraceId, row.SessionId
			count++
		}
	}
	if count == 0 {
		return "", "", fmt.Errorf("no trace matches prefix %q", id)
	}
	if count > 1 {
		return "", "", fmt.Errorf("ambiguous trace prefix %q (%d matches)", id, count)
	}
	spans, err = api.Trace(ctx, foundID, client.Scope(nil, foundSession))
	if err != nil {
		return "", "", err
	}
	if len(spans.Data) == 0 {
		return "", "", fmt.Errorf("no spans found for trace %q", foundID)
	}
	return foundID, foundSession, nil
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

// isTerminal returns true if the given file is a TTY. A minimal stat-based
// check that doesn't pull in golang.org/x/term: character devices are TTYs.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
