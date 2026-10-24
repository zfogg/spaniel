package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Use the real MCP SDK on both ends of the bridge and an explicitly loopback
// HTTP listener. This covers the wire protocol rather than mocking tool calls.
func bridgeHTTPServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestStdioBridgeProtocol(t *testing.T) {
	for _, jsonResponse := range []bool{false, true} {
		t.Run(fmt.Sprintf("JSON=%v", jsonResponse), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			backend := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "backend", Version: "test"}, nil)
			started, cancelled := make(chan struct{}), make(chan struct{})
			mcpsdk.AddTool(backend, &mcpsdk.Tool{Name: "echo"}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in struct{ Text string }) (*mcpsdk.CallToolResult, any, error) {
				return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: in.Text}}}, nil, nil
			})
			mcpsdk.AddTool(backend, &mcpsdk.Tool{Name: "wait"}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return nil, nil, ctx.Err()
			})
			backend.AddResource(&mcpsdk.Resource{URI: "test://resource", Name: "resource"}, func(context.Context, *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
				return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{URI: "test://resource", Text: "resource text"}}}, nil
			})
			backend.AddPrompt(&mcpsdk.Prompt{Name: "prompt"}, func(context.Context, *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
				return &mcpsdk.GetPromptResult{Description: "prompt text"}, nil
			})
			var deletes atomic.Int32
			streamReady := make(chan struct{}, 1)
			handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return backend }, &mcpsdk.StreamableHTTPOptions{JSONResponse: jsonResponse})
			srv := bridgeHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method != http.MethodPost || r.Header.Get("Mcp-Session-Id") != "" {
					if r.Header.Get("MCP-Protocol-Version") == "" {
						t.Error("missing negotiated protocol header")
					}
				}
				if r.Method == http.MethodGet {
					streamReady <- struct{}{}
				}
				if r.Method == http.MethodDelete {
					deletes.Add(1)
				}
				handler.ServeHTTP(w, r)
			}))
			clientTransport, bridgeTransport := mcpsdk.NewInMemoryTransports()
			conn, err := bridgeTransport.Connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- runBridge(ctx, conn, srv.URL, "test-secret", srv.Client()) }()
			changed := make(chan struct{}, 1)
			client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stdio-client", Version: "test"}, &mcpsdk.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcpsdk.ToolListChangedRequest) { changed <- struct{}{} },
			})
			cs, err := client.Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			if cs.InitializeResult().ServerInfo.Name != "backend" {
				t.Fatal("server identity was not preserved")
			}
			if names := toolNames(t, cs); !names["echo"] || !names["wait"] {
				t.Fatalf("missing tools: %v", names)
			}
			res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "echo", Arguments: map[string]any{"Text": "hello"}})
			if err != nil || res.Content[0].(*mcpsdk.TextContent).Text != "hello" {
				t.Fatalf("echo: %v, %v", res, err)
			}
			if _, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "missing"}); err == nil {
				t.Fatal("expected upstream JSON-RPC error")
			}
			resource, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "test://resource"})
			if err != nil || resource.Contents[0].Text != "resource text" {
				t.Fatalf("resource: %v, %v", resource, err)
			}
			prompt, err := cs.GetPrompt(ctx, &mcpsdk.GetPromptParams{Name: "prompt"})
			if err != nil || prompt.Description != "prompt text" {
				t.Fatalf("prompt: %v, %v", prompt, err)
			}
			select {
			case <-streamReady:
			case <-ctx.Done():
				t.Fatal("no notification stream")
			}
			backend.RemoveTools("echo")
			select {
			case <-changed:
			case <-ctx.Done():
				t.Fatal("tool-list notification was not forwarded")
			}
			callCtx, cancelCall := context.WithCancel(ctx)
			callDone := make(chan error, 1)
			go func() { _, err := cs.CallTool(callCtx, &mcpsdk.CallToolParams{Name: "wait"}); callDone <- err }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("waiting tool never started")
			}
			cancelCall()
			if err := <-callDone; !errors.Is(err, context.Canceled) {
				t.Fatalf("call cancellation: %v", err)
			}
			select {
			case <-cancelled:
			case <-ctx.Done():
				t.Fatal("cancellation was not forwarded to the server")
			}
			cs.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("bridge shutdown: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("bridge did not stop after client EOF")
			}
			if deletes.Load() != 1 {
				t.Fatalf("session DELETE count = %d", deletes.Load())
			}
		})
	}
}

func TestStdioBridgeHTTPFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv := bridgeHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret response body", http.StatusUnauthorized)
	}))
	clientTransport, bridgeTransport := mcpsdk.NewInMemoryTransports()
	conn, _ := bridgeTransport.Connect(ctx)
	done := make(chan error, 1)
	go func() { done <- runBridge(ctx, conn, srv.URL, "", srv.Client()) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "test"}, nil)
	if _, err := client.Connect(ctx, clientTransport, nil); err == nil {
		t.Fatal("expected initialization failure")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unexpected diagnostic: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("bridge hung on HTTP error")
	}
}

func TestStdioBridgeEndpoint(t *testing.T) {
	for _, endpoint := range []string{"stdio", "/mcp", "file:///tmp/mcp", "http://user:secret@localhost/mcp", "http://localhost/mcp#fragment"} {
		if err := validateMCPEndpoint(endpoint); err == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
}
