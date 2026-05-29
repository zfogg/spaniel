package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// streamEvent mirrors ws.Event with raw payload bytes so each subcommand can
// decode the payload into its own typed struct.
type streamEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// streamWS dials apiBase's /ws endpoint and pushes parsed events into the
// returned channel. The channel closes when ctx is canceled or the connection
// drops. errCh receives at most one error describing why the stream ended.
func streamWS(ctx context.Context, apiBase string) (<-chan streamEvent, <-chan error, error) {
	wsURL, err := httpToWS(apiBase)
	if err != nil {
		return nil, nil, err
	}
	// coder/websocket uses the dial context only for the handshake; bound it
	// with a 5s timeout, then read with the long-lived stream context.
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	conn, _, err := websocket.Dial(dialCtx, wsURL+"/ws", nil)
	dialCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}
	conn.SetReadLimit(-1) // server events (large attribute JSON, log bodies) may exceed the 32KB default

	out := make(chan streamEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		defer conn.CloseNow()
		// conn.Read returns once ctx is canceled, so no separate closer goroutine
		// is needed.
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if ctx.Err() == nil {
					errCh <- err
				}
				return
			}
			var ev streamEvent
			if err := json.Unmarshal(data, &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, errCh, nil
}

// httpToWS converts an http(s) base URL into the ws(s) equivalent.
func httpToWS(apiBase string) (string, error) {
	u, err := url.Parse(apiBase)
	if err != nil {
		return "", fmt.Errorf("parse api base %q: %w", apiBase, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("api base must be http(s), got %q", u.Scheme)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// severityFromName maps an OTel severity-text string to its numeric level.
// Returns 0 ("unspecified") for unknown inputs. Matches the constants in
// internal/storage: trace=1, debug=5, info=9, warn=13, error=17, fatal=21.
func severityFromName(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return 1
	case "debug":
		return 5
	case "info":
		return 9
	case "warn", "warning":
		return 13
	case "error", "err":
		return 17
	case "fatal":
		return 21
	default:
		return 0
	}
}

func severityName(n int) string {
	switch {
	case n >= 21:
		return "FATAL"
	case n >= 17:
		return "ERROR"
	case n >= 13:
		return "WARN"
	case n >= 9:
		return "INFO"
	case n >= 5:
		return "DEBUG"
	case n >= 1:
		return "TRACE"
	default:
		return "?"
	}
}

// drainAndPrint pulls events off the channel until the channel closes or ctx
// is canceled, calling fn for each event. Returned only for tests' benefit;
// in production this runs until the user ctrl-Cs.
func drainAndPrint(ctx context.Context, w io.Writer, ch <-chan streamEvent, fn func(io.Writer, streamEvent)) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fn(w, ev)
		}
	}
}
