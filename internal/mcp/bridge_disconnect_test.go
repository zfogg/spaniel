package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Deliberately fail the first response write before the reader can observe EOF.
// This makes the shutdown race deterministic, without timing sleeps or retries.
type disconnectConnection struct {
	first    bool
	readErr  error
	writeErr error
	closed   atomic.Bool
}

func (c *disconnectConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if c.readErr != nil {
		return nil, c.readErr
	}
	if !c.first {
		c.first = true
		return jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (c *disconnectConnection) Write(context.Context, jsonrpc.Message) error { return c.writeErr }
func (c *disconnectConnection) Close() error                                 { c.closed.Store(true); return nil }
func (*disconnectConnection) SessionID() string                              { return "" }

func TestStdioBridgeClientDisconnectDuringResponse(t *testing.T) {
	unexpected := errors.New("unexpected transport failure")
	for _, transportErr := range []error{io.EOF, io.ErrClosedPipe, mcpsdk.ErrConnectionClosed, unexpected} {
		t.Run(transportErr.Error(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var deletes atomic.Int32
			srv := bridgeHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deletes.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "disconnect-test")
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
			}))
			conn := &disconnectConnection{writeErr: transportErr}
			err := runBridge(ctx, conn, srv.URL, "", srv.Client())
			if transportErr == unexpected {
				if !errors.Is(err, unexpected) {
					t.Fatalf("unexpected failure hidden: %v", err)
				}
			} else if err != nil {
				t.Fatalf("client disconnect reported as failure: %v", err)
			}
			if !conn.closed.Load() || deletes.Load() != 1 {
				t.Fatalf("shutdown cleanup: closed=%t, deletes=%d", conn.closed.Load(), deletes.Load())
			}
		})
	}
}

func TestStdioBridgeClientReadClosedPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn := &disconnectConnection{readErr: io.ErrClosedPipe}
	if err := runBridge(ctx, conn, "http://127.0.0.1/unused", "", http.DefaultClient); err != nil {
		t.Fatalf("closed client read: %v", err)
	}
}

type closedResponseBody struct{}

func (closedResponseBody) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (closedResponseBody) Close() error             { return nil }

type disconnectRoundTripper struct{}

func (disconnectRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: closedResponseBody{}}, nil
}

func TestStdioBridgeUpstreamClosedPipeRemainsAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := runBridge(ctx, &disconnectConnection{}, "http://127.0.0.1/unused", "", &http.Client{Transport: disconnectRoundTripper{}})
	if !errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), errStdioClientClosed.Error()) {
		t.Fatalf("upstream pipe failure confused with client disconnect: %v", err)
	}
}
