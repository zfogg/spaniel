package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

// RunStdioBridge forwards the original JSON-RPC messages, including initialization,
// capabilities, errors, progress and cancellation. Storage and permission checks
// remain in the running HTTP server. Stdout belongs exclusively to the SDK's
// newline-delimited JSON transport; callers report errors on stderr.
func RunStdioBridge(ctx context.Context, endpoint, token string) error {
	if err := validateMCPEndpoint(endpoint); err != nil {
		return err
	}
	conn, err := (&mcpsdk.StdioTransport{}).Connect(ctx)
	if err != nil {
		return err
	}
	return runBridge(ctx, conn, endpoint, token, &http.Client{Transport: http.DefaultTransport})
}

func validateMCPEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return errors.New("MCP URL must be an absolute http(s) URL without credentials or a fragment")
	}
	return nil
}

type stdioBridge struct {
	conn     mcpsdk.Connection
	client   *http.Client
	endpoint string
	token    string
	mu       sync.Mutex
	session  string
	protocol string
}

func runBridge(ctx context.Context, conn mcpsdk.Connection, endpoint, token string, client *http.Client) error {
	b := &stdioBridge{conn: conn, client: client, endpoint: endpoint, token: token}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer b.closeSession()
	group, ctx := errgroup.WithContext(ctx)
	// Closing the transport also unblocks stdin when HTTP fails or the process
	// receives an interrupt. EOF cancels outstanding HTTP requests and SSE reads.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer conn.Close()
	group.Go(func() error {
		defer cancel()
		initialized := false
		for {
			msg, err := conn.Read(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) || ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read MCP stdin: %w", err)
			}
			post := func() error {
				resp, err := b.post(ctx, msg)
				if err != nil {
					return err
				}
				return b.readResponse(ctx, resp)
			}
			if !initialized {
				// Initialization must finish before we know the negotiated protocol
				// version and can establish the standalone notification stream.
				if err := post(); err != nil {
					return err
				}
				if req, ok := msg.(*jsonrpc.Request); ok && req.Method == "initialize" {
					initialized = true
					group.Go(func() error { return b.listen(ctx) })
				}
			} else if req, ok := msg.(*jsonrpc.Request); ok && req.Method == "notifications/initialized" {
				// Finish initialization before allowing concurrent requests.
				if err := post(); err != nil {
					return err
				}
			} else {
				// A tool can block even its response headers. Keep reading stdin
				// so cancellation and unrelated requests can reach the server.
				group.Go(post)
			}
		}
	})
	err := group.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (b *stdioBridge) request(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, b.endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	b.mu.Lock()
	session, protocol := b.session, b.protocol
	b.mu.Unlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if protocol != "" {
		req.Header.Set("MCP-Protocol-Version", protocol)
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		// Do not include URLs, headers or response bodies in diagnostics.
		return nil, fmt.Errorf("MCP %s failed; check the running server and --url: %w", method, err)
	}
	return resp, nil
}

func (b *stdioBridge) post(ctx context.Context, msg jsonrpc.Message) (*http.Response, error) {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return nil, err
	}
	resp, err := b.request(ctx, http.MethodPost, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("MCP POST returned HTTP %d; check the endpoint and bearer token", resp.StatusCode)
	}
	if session := resp.Header.Get("Mcp-Session-Id"); session != "" {
		b.mu.Lock()
		b.session = session
		b.mu.Unlock()
	}
	return resp, nil
}

func (b *stdioBridge) readResponse(ctx context.Context, resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return err
		}
		return b.forward(ctx, data)
	case "text/event-stream":
		return b.readSSE(ctx, resp.Body)
	default:
		return fmt.Errorf("unsupported MCP response Content-Type %q", mediaType)
	}
}

func (b *stdioBridge) forward(ctx context.Context, data []byte) error {
	msg, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		return fmt.Errorf("decode MCP response: %w", err)
	}
	if resp, ok := msg.(*jsonrpc.Response); ok && resp.Error == nil {
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(resp.Result, &result) == nil && result.ProtocolVersion != "" {
			b.mu.Lock()
			b.protocol = result.ProtocolVersion
			b.mu.Unlock()
		}
	}
	return b.conn.Write(ctx, msg)
}

func (b *stdioBridge) readSSE(ctx context.Context, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 64<<20)
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		return b.forward(ctx, []byte(payload))
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	return scanner.Err()
}

func (b *stdioBridge) listen(ctx context.Context) error {
	resp, err := b.request(ctx, http.MethodGet, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("MCP notification stream returned HTTP %d", resp.StatusCode)
	}
	if err := b.readResponse(ctx, resp); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A lost stream must not leave the stdio client silently missing updates.
	return errors.New("MCP notification stream closed; reconnect the stdio client")
}

func (b *stdioBridge) closeSession() {
	b.mu.Lock()
	hasSession := b.session != ""
	b.mu.Unlock()
	if !hasSession {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if resp, err := b.request(ctx, http.MethodDelete, nil); err == nil {
		resp.Body.Close()
	}
}
