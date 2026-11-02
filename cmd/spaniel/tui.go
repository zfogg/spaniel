package main

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

// tui is a deprecated compatibility entry point with the same flags/behavior.
func tuiSubcommand() *cobra.Command {
	cmd := watchSubcommand()
	cmd.Use = "tui"
	cmd.Deprecated = "use spaniel watch"
	return cmd
}
func muxTUI(ctx context.Context, apiBase string) error {
	return muxTUIWithFilter(ctx, apiBase, spanFilter{}, "")
}
func muxTUIWithFilter(ctx context.Context, apiBase string, f spanFilter, session string) error {
	return watch.Run(ctx, apiBase, watch.Options{SessionID: session, Filters: protocol.Filters{Service: f.Service, ErrorsOnly: f.ErrorsOnly, N1Only: f.N1Only}})
}
