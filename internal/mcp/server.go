// Package mcp exposes Spaniel's observability data to MCP (Model Context
// Protocol) clients such as Claude Code and Claude Desktop. It runs in-process
// on the main HTTP server (mounted at /mcp) over the streamable-HTTP transport
// and reads directly from storage — no second process, so it never contends
// with the server for DuckDB's single writer lock.
//
// This file is the foundation: server construction, the telemetry middleware,
// and a single read-only get_server_info tool. Subsequent tools live in
// sibling files in this package and are registered from NewHandler.
package mcp

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/storage"
)

// Options configures the MCP server.
type Options struct {
	// Version is the spaniel build version, reported in the MCP handshake and by
	// get_server_info.
	Version string
	// AllowWrites gates registration of the mutating tools (create/activate
	// session, set baseline, prune). When false, those tools are not registered
	// at all — they do not appear in tools/list and cannot be called.
	AllowWrites bool
}

// handler holds the dependencies shared by every tool handler.
type handler struct {
	store  *storage.DB
	opts   Options
	tracer trace.Tracer
}

// NewHandler builds the MCP server and returns an http.Handler serving the
// streamable-HTTP transport. Mount it at /mcp on the main server.
func NewHandler(store *storage.DB, opts Options) http.Handler {
	h := &handler{
		store:  store,
		opts:   opts,
		tracer: otel.Tracer("spaniel.mcp"),
	}

	impl := &mcpsdk.Implementation{
		Name:       "spaniel",
		Title:      "Spaniel — local OpenTelemetry viewer",
		Version:    opts.Version,
		WebsiteURL: "https://github.com/zfogg/spaniel",
	}
	server := mcpsdk.NewServer(impl, nil)

	// One receiving middleware instruments every MCP method (and names tool
	// calls by tool) so MCP usage shows up as spans in Spaniel itself.
	server.AddReceivingMiddleware(h.telemetryMiddleware)

	h.registerTools(server)

	// One server instance, shared across all sessions.
	//
	// DisableLocalhostProtection turns off the SDK's DNS-rebinding guard, which
	// otherwise rejects loopback-arriving requests whose Host header isn't
	// localhost. Spaniel is explicitly meant to be reachable beyond loopback
	// (bind_address_v4/v6 can be a LAN IP or 0.0.0.0, and it may sit behind a
	// reverse proxy with a hostname); the existing bearer-token auth is the
	// access control, not the Host header.
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	}, &mcpsdk.StreamableHTTPOptions{DisableLocalhostProtection: true})
}

// registerTools wires up the tool set. Read tools are always registered; write
// tools are added by registerWriteTools only when AllowWrites is set. Tool
// groups are split across files in this package; this is the single entry point.
func (h *handler) registerTools(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "get_server_info",
		Description: "Get Spaniel server version and a summary of stored telemetry (span/trace/log/session counts, DB size, the active session). Call this first to confirm the connection and see how much data is available.",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, h.getServerInfo)

	h.registerTraceTools(s)
	h.registerDiagnosticTools(s)
	h.registerLogTools(s)
	h.registerTopologyTools(s)

	if h.opts.AllowWrites {
		h.registerWriteTools(s)
	}
}

// registerWriteTools is implemented alongside the write tools (issue #103).
// Until those land it is a no-op so AllowWrites can be exercised end-to-end.
func (h *handler) registerWriteTools(_ *mcpsdk.Server) {}

// telemetryMiddleware starts an OTel span around every received MCP method,
// naming tool calls after the invoked tool.
func (h *handler) telemetryMiddleware(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		name := "mcp." + method
		if p, ok := req.GetParams().(*mcpsdk.CallToolParamsRaw); ok && p != nil {
			name = "mcp.tool/" + p.Name
		}
		ctx, span := h.tracer.Start(ctx, name)
		defer span.End()
		res, err := next(ctx, method, req)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return res, err
	}
}

// emptyInput is the input type for tools that take no arguments. AddTool infers
// an object schema with no properties from it.
type emptyInput struct{}

// ServerInfo is the output of get_server_info.
type ServerInfo struct {
	Version            string `json:"version" jsonschema:"spaniel build version"`
	ActiveSessionID    string `json:"active_session_id" jsonschema:"id of the session new telemetry is written to"`
	ActiveSessionLabel string `json:"active_session_label" jsonschema:"human-readable label of the active session"`
	WritesEnabled      bool   `json:"writes_enabled" jsonschema:"whether mutating tools are available (server started with --mcp-allow-writes)"`
	SpanCount          int    `json:"span_count"`
	TraceCount         int    `json:"trace_count"`
	LogCount           int    `json:"log_count"`
	SessionCount       int    `json:"session_count"`
	DBSizeBytes        int64  `json:"db_size_bytes"`
}

func (h *handler) getServerInfo(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, ServerInfo, error) {
	stats, err := h.store.GetStats("")
	if err != nil {
		return nil, ServerInfo{}, fmt.Errorf("read stats: %w", err)
	}
	info := ServerInfo{
		Version:            h.opts.Version,
		ActiveSessionID:    h.store.ActiveSessionID(),
		ActiveSessionLabel: h.store.ActiveSessionLabel(),
		WritesEnabled:      h.opts.AllowWrites,
		SpanCount:          stats.SpanCount,
		TraceCount:         stats.TraceCount,
		LogCount:           stats.LogCount,
		SessionCount:       stats.SessionCount,
		DBSizeBytes:        stats.DBSize,
	}
	return nil, info, nil
}
