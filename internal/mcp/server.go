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
	"strings"
	"sync"

	"github.com/zfogg/gotel/go/mcptrace"
	"github.com/zfogg/spaniel/internal/telemetry"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// Options configures the MCP server.
type Options struct {
	// Version is the spaniel build version, reported in the MCP handshake and by
	// get_server_info.
	Version string
	// PublicURL overrides the request origin when advertising HTTP links.
	PublicURL string
	// AllowWrites gates registration of the mutating tools (create/activate
	// session, set baseline, prune). When false, those tools are not registered
	// at all — they do not appear in tools/list and cannot be called.
	AllowWrites bool
	// AllowSQLWrites independently enables mutations through query_sql.
	AllowSQLWrites bool
	// Runtime dependencies are optional so tests and storage-only embeddings can
	// still construct the MCP server. When supplied they enrich operational
	// tools and preserve the same live behavior as the HTTP API.
	Hub        *ws.Hub
	Manifests  *coverage.Manifests
	Forwarder  *forwarder.Forwarder
	Throughput ThroughputProvider
	Drops      DropCounterProvider
	Sources    SourcesProvider
}

type ThroughputProvider interface{ Throughput() storage.Throughput }
type DropCounterProvider interface {
	DroppedSpansTotal() int64
	DroppedLogsTotal() int64
	DroppedMetricPointsTotal() int64
	LastDropAt() int64
}
type SourcesProvider interface{ Sources() []storage.SourceStats }

// Handler serves MCP and can replace its underlying server when write access
// changes. Replacing the server intentionally invalidates existing MCP
// sessions: clients reconnect and receive a fresh tool list with the new
// permissions instead of retaining a writable session after writes are
// disabled.
type Handler struct {
	mu      sync.RWMutex
	store   *storage.DB
	opts    Options
	current http.Handler
}

// handler holds the dependencies shared by every tool handler.
type handler struct {
	store *storage.DB
	opts  Options
}

type requestOriginKey struct{}

// NewHandler builds a reloadable MCP HTTP handler. Mount it at /mcp on the
// main server.
func NewHandler(store *storage.DB, opts Options) *Handler {
	h := &Handler{store: store, opts: opts}
	h.current = buildHandler(store, opts)
	return h
}

// ServeHTTP dispatches to the current MCP server.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	current := h.current
	h.mu.RUnlock()
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	ctx := context.WithValue(r.Context(), requestOriginKey{}, scheme+"://"+r.Host)
	current.ServeHTTP(w, r.WithContext(ctx))
}

// SetAllowWrites hot-applies MCP write access. Rebuilding is required because
// write tools are omitted entirely from a read-only server's tools/list.
func (h *Handler) SetAllowWrites(allow bool) {
	h.mu.RLock()
	unchanged := h.opts.AllowWrites == allow
	h.mu.RUnlock()
	if unchanged {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.opts.AllowWrites == allow {
		return
	}
	h.opts.AllowWrites = allow
	h.current = buildHandler(h.store, h.opts)
}

func buildHandler(store *storage.DB, opts Options) http.Handler {
	h := &handler{
		store: store,
		opts:  opts,
	}

	impl := &mcpsdk.Implementation{
		Name:       "spaniel",
		Title:      "Spaniel - tools for OpenTelemetry and logging, to help with local development",
		Version:    opts.Version,
		WebsiteURL: "https://github.com/zfogg/spaniel",
	}
	server := mcpsdk.NewServer(impl, nil)

	// One receiving middleware instruments every MCP method (and names tool
	// calls by tool) so MCP usage shows up as spans in Spaniel itself.
	h.registerTools(server)
	var tools []string
	registeredToolNames.Range(func(key, _ any) bool {
		tools = append(tools, key.(string))
		return true
	})
	middleware, err := mcptrace.New(mcptrace.Options{
		Prefix: "spaniel", Tools: tools, Context: telemetry.WithMCPService,
	})
	if err != nil {
		panic(fmt.Errorf("create MCP instrumentation: %w", err))
	}
	server.AddReceivingMiddleware(middleware)

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
	addTOONTool(s, &mcpsdk.Tool{
		Name:        "get_server_info",
		Description: "Get Spaniel server version, the OpenAPI specification URL, and a summary of stored telemetry (span/trace/log/session counts, DB size, the active session). Call this first to confirm the connection and see how much data is available.",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, h.getServerInfo)

	h.registerTraceTools(s)
	h.registerDiagnosticTools(s)
	h.registerLogTools(s)
	h.registerTopologyTools(s)
	h.registerAgentTools(s)
	h.registerSessionTools(s)
	h.registerSQLTool(s)
	h.registerDashboardTools(s)
	addTOONTool(s, &mcpsdk.Tool{Name: "format_sql", Description: "Format SQL layout without executing it. Preserves comments, literals, and bind placeholders.", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}, h.formatSQL)
	h.registerAlertTools(s)
	h.registerCoverageTools(s)
	h.registerNotificationTools(s)
	h.registerIngestionTools(s)
	h.registerResourcesAndPrompts(s)

	if h.opts.AllowWrites {
		h.registerWriteTools(s)
	}
}

// emptyInput is the input type for tools that take no arguments. AddTool infers
// an object schema with no properties from it.
type emptyInput struct{}

// ServerInfo is the output of get_server_info.
type ServerInfo struct {
	Version            string `json:"version" jsonschema:"spaniel build version"`
	OpenAPIURL         string `json:"openapi_url" jsonschema:"absolute URL of the OpenAPI JSON specification"`
	ActiveSessionID    string `json:"active_session_id" jsonschema:"id of the session new telemetry is written to"`
	ActiveSessionLabel string `json:"active_session_label" jsonschema:"human-readable label of the active session"`
	WritesEnabled      bool   `json:"writes_enabled" jsonschema:"whether mutating tools are available (server started with --mcp-allow-writes)"`
	SQLWritesEnabled   bool   `json:"sql_writes_enabled" jsonschema:"whether query_sql permits mutations"`
	SpanCount          int    `json:"span_count"`
	TraceCount         int    `json:"trace_count"`
	LogCount           int    `json:"log_count"`
	SessionCount       int    `json:"session_count"`
	DBSizeBytes        int64  `json:"db_size_bytes"`
}

func (h *handler) getServerInfo(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, ServerInfo, error) {
	stats, err := h.store.WithContext(ctx).GetStats("")
	if err != nil {
		return nil, ServerInfo{}, fmt.Errorf("read stats: %w", err)
	}
	publicURL := h.opts.PublicURL
	if publicURL == "" {
		publicURL, _ = ctx.Value(requestOriginKey{}).(string)
	}
	info := ServerInfo{
		Version:            h.opts.Version,
		OpenAPIURL:         strings.TrimRight(publicURL, "/") + "/api/openapi.json",
		ActiveSessionID:    h.store.ActiveSessionID(),
		ActiveSessionLabel: h.store.ActiveSessionLabel(),
		WritesEnabled:      h.opts.AllowWrites,
		SQLWritesEnabled:   h.opts.AllowSQLWrites,
		SpanCount:          stats.SpanCount,
		TraceCount:         stats.TraceCount,
		LogCount:           stats.LogCount,
		SessionCount:       stats.SessionCount,
		DBSizeBytes:        stats.DBSize,
	}
	return nil, info, nil
}
