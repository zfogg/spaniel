package main

import (
	"context"
	"github.com/spf13/cobra"
	"github.com/zfogg/spaniel/internal/watch"
	"github.com/zfogg/spaniel/internal/watch/protocol"
)

// Register the sole public entry point for the complete terminal dashboard.
func registerTUICommand(root *cobra.Command) { root.AddCommand(tuiSubcommand()) }
func muxTUI(ctx context.Context, apiBase string) error {
	return muxTUIWithFilter(ctx, apiBase, spanFilter{}, "")
}
func muxTUIWithFilter(ctx context.Context, apiBase string, f spanFilter, session string) error {
	return watch.Run(ctx, apiBase, watch.Options{SessionID: session, Filters: protocol.Filters{Service: f.Service, ErrorsOnly: f.ErrorsOnly, N1Only: f.N1Only}})
}
