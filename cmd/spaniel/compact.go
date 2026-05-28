package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

type compactResult struct {
	BytesBefore int64 `json:"bytes_before"`
	BytesAfter  int64 `json:"bytes_after"`
	Reclaimed   int64 `json:"reclaimed"`
}

func compactSubcommand() *cobra.Command {
	var apiBase string
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Run CHECKPOINT + VACUUM on the Spaniel database",
		Long: `Triggers a CHECKPOINT followed by a VACUUM on the local DuckDB file.
This returns free pages to the OS and removes WAL overhead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := http.Post(apiBase+"/api/settings/compact", "application/json", http.NoBody)
			if err != nil {
				return fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				return fmt.Errorf("compact: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			var env struct {
				Data compactResult `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			r := env.Data
			fmt.Fprintf(cmd.OutOrStdout(), "compaction complete\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  before:    %s\n", humanBytes(r.BytesBefore))
			fmt.Fprintf(cmd.OutOrStdout(), "  after:     %s\n", humanBytes(r.BytesAfter))
			fmt.Fprintf(cmd.OutOrStdout(), "  reclaimed: %s\n", humanBytes(r.Reclaimed))
			return nil
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.SilenceUsage = true
	return cmd
}
