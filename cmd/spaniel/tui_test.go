package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestTUIIsSoleCanonicalTerminalCommand(t *testing.T) {
	root := &cobra.Command{Use: "spaniel"}
	registerTUICommand(root)
	command, _, err := root.Find([]string{"tui"})
	if err != nil || command.Name() != "tui" || command.Deprecated != "" {
		t.Fatal(command, err)
	}
	for _, flag := range []string{"api", "session", "service", "errors-only", "n+1-only"} {
		if command.Flags().Lookup(flag) == nil {
			t.Fatal("missing tui flag", flag)
		}
	}
	for _, command := range root.Commands() {
		if command.Name() == "watch" {
			t.Fatal("removed watch command remains registered")
		}
		for _, alias := range command.Aliases {
			if alias == "watch" {
				t.Fatal("removed watch alias remains")
			}
		}
	}
	var help bytes.Buffer
	root.SetOut(&help)
	root.SetErr(&help)
	root.SetArgs([]string{"tui", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help.String(), "spaniel tui") || strings.Contains(help.String(), "deprecated") || strings.Contains(help.String(), "spaniel watch") {
		t.Fatal(help.String())
	}
	root.SetArgs([]string{"watch"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatal("watch was accepted", err)
	}
}

func TestTUICommandPipedFiltersAndShutdown(t *testing.T) {
	if isTerminal(os.Stdout) {
		t.Skip("requires piped stdout")
	}
	frames := []map[string]any{
		{"type": "span", "payload": map[string]any{"serviceName": "wanted", "name": "wanted-error", "durationNs": 1000000, "statusCode": 2}},
		{"type": "span", "payload": map[string]any{"serviceName": "other", "name": "other-error", "durationNs": 1000000, "statusCode": 2}},
		{"type": "span", "payload": map[string]any{"serviceName": "wanted", "name": "wanted-ok", "durationNs": 1000000, "statusCode": 1}},
	}
	server, wrote := newWSPusher(t, frames)
	defer server.Close()
	var output syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := tuiSubcommand()
	command.SetOut(&output)
	command.SetArgs([]string{"--api", server.URL, "--service", "wanted", "--errors-only", "--session", "selected"})
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	wrote.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(output.String(), "wanted-error") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tui stream did not stop")
	}
	if text := output.String(); !strings.Contains(text, "wanted-error") || strings.Contains(text, "wanted-ok") || strings.Contains(text, "other-error") {
		t.Fatal(text)
	}
}
