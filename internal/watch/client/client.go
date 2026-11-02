// Package client provides the terminal's read-only HTTP API and single live
// subscription. It never imports storage or owns an ingestion pipeline.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/zfogg/spaniel/internal/liveevent"
)

type Options struct {
	HTTP                                        *http.Client
	Timeout, MinBackoff, MaxBackoff, StaleAfter time.Duration
	Queue                                       int
}
type Client struct {
	base    *url.URL
	http    *http.Client
	options Options
	running atomic.Bool
}
type Status struct {
	Connected bool
	Err       error
	Epoch     uint64
	Resync    bool
}
type Update struct {
	Event  *liveevent.Message
	Status *Status
}
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message) }

func New(base string, o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("API base must be an absolute http(s) URL without query or fragment")
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{}
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MinBackoff <= 0 {
		o.MinBackoff = 500 * time.Millisecond
	}
	if o.MaxBackoff < o.MinBackoff {
		o.MaxBackoff = max(o.MinBackoff, 30*time.Second)
	}
	if o.StaleAfter <= 0 {
		o.StaleAfter = 15 * time.Second
	}
	if o.Queue <= 0 {
		o.Queue = 256
	}
	return &Client{base: u, http: o.HTTP, options: o}, nil
}

// Read decodes a complete API envelope. Requests and response bodies are bounded
// even when the supplied HTTP client has no timeout. Preview is the sole POST.
func (c *Client) read(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := *c.base
	u.RawPath = strings.TrimRight(u.EscapedPath(), "/") + path
	var pathErr error
	u.Path, pathErr = url.PathUnescape(u.RawPath)
	if pathErr != nil {
		return pathErr
	}
	u.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, c.options.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{resp.StatusCode, strings.TrimSpace(string(b))}
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 32<<20))
	decoder.UseNumber()
	return decoder.Decode(out)
}

// Stream may be called once at a time. Cancellation closes the connection and
// output channel. A full bounded queue forces reconnect/resync instead of
// silently continuing after dropped durable changes.
func (c *Client) Stream(ctx context.Context) (<-chan Update, error) {
	if !c.running.CompareAndSwap(false, true) {
		return nil, errors.New("live subscription already running")
	}
	out := make(chan Update, c.options.Queue)
	go func() { defer c.running.Store(false); defer close(out); c.run(ctx, out) }()
	return out, nil
}
func (c *Client) run(ctx context.Context, out chan Update) {
	send := func(u Update) bool {
		select {
		case out <- u:
			return true
		case <-ctx.Done():
			return false
		}
	}
	u := *c.base
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ws"
	u.RawQuery = "ready=1"
	backoff := c.options.MinBackoff
	var epoch uint64
	for ctx.Err() == nil {
		dialCtx, cancel := context.WithTimeout(ctx, c.options.Timeout)
		conn, resp, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{HTTPClient: c.http})
		cancel()
		if err != nil && resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err == nil {
			conn.SetReadLimit(8 << 20)
			// The server's initial heartbeat confirms registration before HTTP
			// bootstrap. Older servers confirm on their first heartbeat/data frame.
			connected := false
			started := time.Now()
			for ctx.Err() == nil {
				readCtx, stop := context.WithTimeout(ctx, c.options.StaleAfter)
				_, data, readErr := conn.Read(readCtx)
				stop()
				if readErr != nil {
					err = readErr
					break
				}
				ev, decodeErr := liveevent.Decode(data)
				if decodeErr != nil {
					continue
				}
				if !connected {
					epoch++
					connected = true
					if !send(Update{Status: &Status{Connected: true, Epoch: epoch, Resync: true}}) {
						break
					}
				}
				if _, known := liveevent.Payloads[ev.Type]; !known {
					continue
				}
				select {
				case out <- Update{Event: &ev}:
				default:
					err = errors.New("live queue overflow; resynchronizing")
				}
				if err != nil {
					break
				}
			}
			conn.CloseNow()
			if time.Since(started) >= c.options.StaleAfter {
				backoff = c.options.MinBackoff
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !send(Update{Status: &Status{Err: err, Epoch: epoch}}) {
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, c.options.MaxBackoff)
	}
}
