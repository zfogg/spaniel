package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/zfogg/spaniel/internal/mcp"
)

func mcpSubcommand() *cobra.Command {
	var endpoint, tokenEnv string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Bridge MCP over stdio to a running Spaniel server",
		Long: "Forward MCP messages between stdin/stdout and a running Spaniel HTTP endpoint. " +
			"The server must already be running; this command does not open the database or start listeners.",
		Args: cobra.NoArgs,
		// Avoid configuration initialization and its diagnostics on stdout.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, _ []string) error {
			var token string
			if tokenEnv != "" {
				token = os.Getenv(tokenEnv)
				if cmd.Flags().Changed("bearer-token-env") && token == "" {
					return fmt.Errorf("bearer token environment variable %q is empty", tokenEnv)
				}
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return mcp.RunStdioBridge(ctx, endpoint, token)
		},
	}
	cmd.Flags().StringVar(&endpoint, "url", "http://localhost:8080/mcp", "Spaniel Streamable HTTP MCP endpoint")
	cmd.Flags().StringVar(&tokenEnv, "bearer-token-env", "SPANIEL_BEARER_TOKEN", "Environment variable containing the server bearer token")
	return cmd
}
