package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// spanEntry is the wire shape of ws.SpanPayload.
type spanEntry struct {
	TraceID     string `json:"traceId"`
	SpanID      string `json:"spanId"`
	ServiceName string `json:"serviceName"`
	Name        string `json:"name"`
	DurationNs  int64  `json:"durationNs"`
	StatusCode  int    `json:"statusCode"`
}

// issueEntry is the wire shape of ws.IssuePayload.
type issueEntry struct {
	TraceID     string `json:"traceId"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	WastedNs    int64  `json:"wastedNs"`
}

// watchSubcommand routes to the Bubble Tea TUI when stdout is a terminal,
// and falls back to the original one-line-per-span printer when stdout is a
// pipe — so `spaniel watch | grep` still does the right thing.
func watchSubcommand() *cobra.Command {
	var (
		apiBase    string
		service    string
		errorsOnly bool
		n1Only     bool
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Live ticker: span/issue feed (interactive table on a TTY, line-stream on a pipe)",
		RunE: func(cmd *cobra.Command, args []string) error {
			f := spanFilter{
				Service:    service,
				ErrorsOnly: errorsOnly,
				N1Only:     n1Only,
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if isTerminal(os.Stdout) {
				return watchTUI(ctx, apiBase, f)
			}
			return runWatch(ctx, cmd.OutOrStdout(), apiBase, f)
		},
	}
	cmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	cmd.Flags().StringVar(&service, "service", "", "Filter: only show spans from this service")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "Only show spans with status_code >= ERROR")
	cmd.Flags().BoolVar(&n1Only, "n+1-only", false, "Only show N+1 detector events (skips regular spans)")
	cmd.SilenceUsage = true
	return cmd
}

type spanFilter struct {
	Service    string
	ErrorsOnly bool
	N1Only     bool
}

func (f spanFilter) matchSpan(s *spanEntry) bool {
	if f.N1Only {
		return false
	}
	if f.Service != "" && s.ServiceName != f.Service {
		return false
	}
	if f.ErrorsOnly && s.StatusCode < 2 {
		return false
	}
	return true
}

func (f spanFilter) matchIssue(_ *issueEntry) bool {
	// Issues are always shown unless the user explicitly filtered them out
	// via --errors-only without --n+1-only.
	if f.ErrorsOnly && !f.N1Only {
		return false
	}
	return true
}

// runWatch is the non-TTY (piped) one-line-per-span path. Kept so
// `spaniel watch | grep 'POST /pay'` still works. No color — pipes aren't
// terminals.
func runWatch(ctx context.Context, w io.Writer, apiBase string, filter spanFilter) error {
	ch, errCh, err := streamWS(ctx, apiBase)
	if err != nil {
		return err
	}

	drainAndPrint(ctx, w, ch, func(w io.Writer, ev streamEvent) {
		switch ev.Type {
		case "span":
			var s spanEntry
			if err := json.Unmarshal(ev.Payload, &s); err != nil {
				return
			}
			if !filter.matchSpan(&s) {
				return
			}
			fmt.Fprintln(w, formatSpan(&s))
		case "issue":
			var iss issueEntry
			if err := json.Unmarshal(ev.Payload, &iss); err != nil {
				return
			}
			if !filter.matchIssue(&iss) {
				return
			}
			fmt.Fprintln(w, formatIssue(&iss))
		}
	})

	select {
	case e := <-errCh:
		if e != nil && ctx.Err() == nil {
			return e
		}
	default:
	}
	return nil
}

// formatSpan / formatIssue produce the line-stream form for the piped
// (non-TTY) path.
func formatSpan(s *spanEntry) string {
	now := time.Now().Format("15:04:05")
	status := "ok"
	if s.StatusCode >= 2 {
		status = "error"
	}
	name := s.Name
	if len(name) > 48 {
		name = name[:47] + "…"
	}
	return fmt.Sprintf("%s %-16s %-50s %8s  %s", now, s.ServiceName, name, humanNs(s.DurationNs), status)
}

func issueKindLabel(k string) string {
	switch k {
	case "n_plus_one":
		return "N+1"
	default:
		return strings.ToUpper(k)
	}
}

func formatIssue(i *issueEntry) string {
	now := time.Now().Format("15:04:05")
	kind := issueKindLabel(i.Kind)
	return fmt.Sprintf("%s [%s] trace=%s  count=%d  wasted=%s",
		now, kind, i.TraceID[:min(8, len(i.TraceID))], i.Count, humanNs(i.WastedNs))
}
