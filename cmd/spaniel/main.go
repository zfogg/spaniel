package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/http/pprof"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/zfogg/spaniel/frontend"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/goroutine"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/mcp"
	"github.com/zfogg/spaniel/internal/receiver"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/telemetry"
	"github.com/zfogg/spaniel/internal/ws"
)

// version is overridden at release-build time via -ldflags "-X main.version=...".
// The Makefile sets it from `git describe --tags`. When unset (e.g. `go run`),
// init() falls back to the VCS metadata Go embeds in the binary.
var version = "dev"

// spaHandler serves a React SPA with client-side routing.
// Falls back to index.html for any request that doesn't match a static asset.
type spaHandler struct {
	distFS fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	// If root, serve index.html
	if path == "" {
		path = "index.html"
	}

	// For assets with extensions (js, css, svg, png, jpg, woff, etc.), check if they exist
	if strings.Contains(path, ".") {
		if _, err := fs.Stat(h.distFS, path); err == nil {
			// File exists, serve it with FileServer
			fileServer := http.FileServer(http.FS(h.distFS))
			fileServer.ServeHTTP(w, r)
			return
		}
		// Asset not found, still fall back to index.html for SPA
	}

	// For routes (no extension) or missing assets, serve index.html for SPA routing
	indexFile, err := h.distFS.Open("index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusNotFound)
		return
	}
	defer indexFile.Close()

	// Read and serve index.html
	indexData, err := io.ReadAll(indexFile)
	if err != nil {
		http.Error(w, "error reading index.html", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(indexData)
}

func init() {
	if version != "dev" {
		return // pinned by ldflags (release build)
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// `go install module@vX.Y.Z` records the tag here; local builds get "(devel)".
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
		return
	}
	// Fall back to the embedded commit hash + dirty flag.
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		if modified == "true" {
			rev += "-dirty"
		}
		version = rev
	}
}

func main() {
	v := viper.New()

	var (
		// flag variables — overrides over config file values when explicitly set
		port              int
		dev               bool
		dbPath            string
		noBrowser         bool
		apiBase           string
		retentionDays     int
		maxSessions       int
		maxDBSizeMB       int
		forwardURLs       []string
		routesFile        string
		tlsCert           string
		tlsKey            string
		bearerToken       string
		sampleRate        int
		sampleAlwaysKeep  string
		sourceRPS         float64
		sourceBurst       int
		forwardSpoolDir   string
		forwardMaxSpoolMB int
		forwardRetryMax   time.Duration
		debugMode         bool
		mcpEnabled        bool
		mcpAllowWrites    bool
	)

	root := &cobra.Command{
		Use:     "spaniel",
		Version: version,
		Short:   "Local OpenTelemetry collector and viewer",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			initViper(v)
			bindRootFlags(v, cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs, tlsCert, tlsKey, bearerToken)
			cfg.RoutesFile = routesFile
			if f := cmd.Flags().Lookup("sample-rate"); f != nil && f.Changed {
				cfg.SampleRate = sampleRate
			}
			if f := cmd.Flags().Lookup("sample-always-keep"); f != nil && f.Changed {
				cfg.SampleAlwaysKeep = sampleAlwaysKeep
			}
			if f := cmd.Flags().Lookup("source-rps"); f != nil && f.Changed {
				cfg.SourceRPS = sourceRPS
			}
			if f := cmd.Flags().Lookup("source-burst"); f != nil && f.Changed {
				cfg.SourceBurst = sourceBurst
			}
			if f := cmd.Flags().Lookup("debug"); f != nil && f.Changed {
				cfg.Debug = debugMode
			}
			if f := cmd.Flags().Lookup("mcp-enabled"); f != nil && f.Changed {
				cfg.MCPEnabled = mcpEnabled
			}
			if f := cmd.Flags().Lookup("mcp-allow-writes"); f != nil && f.Changed {
				cfg.MCPAllowWrites = mcpAllowWrites
			}
			return run(cfg)
		},
	}

	root.PersistentFlags().StringVar(&dbPath, "db-path", "", "Path to DuckDB file")
	root.PersistentFlags().IntVar(&retentionDays, "retention", 0, "Delete sessions older than N days (0 = use config)")
	root.PersistentFlags().IntVar(&maxSessions, "max-sessions", 0, "Keep at most N sessions (0 = use config)")
	root.PersistentFlags().IntVar(&maxDBSizeMB, "max-db-size", 0, "Shrink DB to at most N MB (0 = use config)")
	root.Flags().IntVar(&port, "port", 0, "HTTP server port (default 8080)")
	root.Flags().BoolVar(&dev, "dev", false, "Proxy UI to Vite dev server on :5173")
	root.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open browser on startup")
	root.Flags().StringArrayVar(&forwardURLs, "forward", nil, "Forward OTLP to this URL (repeatable, e.g. http://tempo:4318)")
	root.Flags().StringVar(&forwardSpoolDir, "forward-spool-dir", "", "Spool directory for forwarding retry buffer (default ~/.spaniel/forward)")
	root.Flags().IntVar(&forwardMaxSpoolMB, "forward-max-spool-mb", 0, "Max spool size per upstream in MB, 0 = 100 MB default")
	root.Flags().DurationVar(&forwardRetryMax, "forward-retry-max", 0, "Max retry backoff for forwarding, 0 = 30s default")
	root.Flags().StringVar(&routesFile, "routes-file", "", "OpenAPI/proto spec file used as the coverage denominator")
	root.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file (PEM)")
	root.Flags().StringVar(&tlsKey, "tls-key", "", "TLS key file (PEM)")
	root.Flags().StringVar(&bearerToken, "bearer-token", "", "Require this bearer token on all requests (env: SPANIEL_BEARER_TOKEN)")
	root.Flags().IntVar(&sampleRate, "sample-rate", 0, "Max uninteresting spans/sec to store (0 = unlimited)")
	root.Flags().StringVar(&sampleAlwaysKeep, "sample-always-keep", "error,n_plus_one,slow", "Signals that bypass the rate limit: error,n_plus_one,slow,none")
	root.Flags().Float64Var(&sourceRPS, "source-rps", 0, "Max spans/sec per service.name source (0 = unlimited)")
	root.Flags().IntVar(&sourceBurst, "source-burst", 0, "Per-source burst capacity (0 = source-rps * 5)")
	root.Flags().BoolVar(&debugMode, "debug", false, "Mount /debug/pprof profiling endpoints")
	root.Flags().BoolVar(&mcpEnabled, "mcp-enabled", true, "Serve the MCP endpoint at /mcp for Claude Code/Desktop")
	root.Flags().BoolVar(&mcpAllowWrites, "mcp-allow-writes", false, "Allow MCP clients to mutate state (create/activate sessions, set baseline, prune)")

	// session subcommand
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage spaniel sessions",
		// CLI session ops don't benefit from usage dumps on every server-side
		// refusal (e.g. "cannot delete the active session" 400). The error
		// itself is the human-facing signal.
		SilenceUsage: true,
	}
	sessionCmd.PersistentFlags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")

	sessionNewCmd := &cobra.Command{
		Use:   "new [label]",
		Short: "Create a new session and activate it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := time.Now().Format("session_2006-01-02_15:04")
			if len(args) > 0 && args[0] != "" {
				label = args[0]
			}
			return sessionNew(apiBase, label)
		},
	}
	sessionCmd.AddCommand(sessionNewCmd)

	sessionListCmd := &cobra.Command{
		Use:   "list",
		Short: "List all sessions in an interactive picker (Enter activates, b baselines, d deletes)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal(os.Stdout) {
				return fmt.Errorf("spaniel session list requires a terminal — run it interactively")
			}
			return sessionListTUI(apiBase)
		},
	}
	sessionCmd.AddCommand(sessionListCmd)

	sessionActivateCmd := &cobra.Command{
		Use:   "activate <id|label>",
		Short: "Activate a session by id or label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sessionActivate(apiBase, args[0], cmd.OutOrStdout())
		},
	}
	sessionCmd.AddCommand(sessionActivateCmd)

	sessionBaselineCmd := &cobra.Command{
		Use:   "baseline [id|label]",
		Short: "Toggle the is_baseline flag on a session (defaults to active)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			return sessionBaseline(apiBase, ref, cmd.OutOrStdout())
		},
	}
	sessionCmd.AddCommand(sessionBaselineCmd)

	sessionDeleteCmd := &cobra.Command{
		Use:   "delete <id|label>",
		Short: "Delete a session (cannot delete the active session)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sessionDelete(apiBase, args[0], cmd.OutOrStdout())
		},
	}
	sessionCmd.AddCommand(sessionDeleteCmd)

	// cobra's SilenceUsage on the parent doesn't cascade to subcommand RunE
	// errors, so flip it on each leaf explicitly. Server-side refusals are
	// self-explanatory; we don't need a usage dump every time.
	for _, c := range []*cobra.Command{
		sessionListCmd, sessionActivateCmd, sessionBaselineCmd, sessionDeleteCmd, sessionNewCmd,
	} {
		c.SilenceUsage = true
	}

	root.AddCommand(sessionCmd)

	// import subcommand
	var importFormat string
	importCmd := &cobra.Command{
		Use:   "import <session_name> <file>",
		Short: "Import a trace file as a baseline session",
		Long: `Import a trace from an OTLP JSON or Jaeger JSON export file.
Use '-' as the file argument to read from stdin.

Examples:
  spaniel import prod-baseline trace.json
  spaniel import prod-baseline - < trace.json
  spaniel import prod-baseline trace.json --format jaeger`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sessionImport(apiBase, args[0], args[1], importFormat)
		},
	}
	importCmd.Flags().StringVar(&importFormat, "format", "auto", "Trace format: auto, otlp, jaeger")
	importCmd.Flags().StringVar(&apiBase, "api", "http://localhost:8080", "Spaniel API base URL")
	root.AddCommand(importCmd)

	// prune subcommand
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Apply the retention policy now and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs, "", "", "")
			return prune(cfg.DBPath, retentionConfig(cfg.RetentionDays, cfg.MaxSessions, cfg.MaxDBSizeMB))
		},
	}
	root.AddCommand(pruneCmd)

	// reset subcommand
	var resetYes bool
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe all spaniel data and start fresh",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !resetYes {
				return fmt.Errorf("refusing to wipe data without --yes")
			}
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs, "", "", "")
			return reset(cfg.DBPath)
		},
	}
	resetCmd.Flags().BoolVar(&resetYes, "yes", false, "Confirm: yes, delete everything")
	root.AddCommand(resetCmd)

	// config subcommand
	root.AddCommand(configSubcommand(v))
	root.AddCommand(ciSubcommand())
	root.AddCommand(diffSubcommand())
	root.AddCommand(doctorSubcommand(v))
	root.AddCommand(traceSubcommand())
	root.AddCommand(logsSubcommand())
	root.AddCommand(watchSubcommand())
	root.AddCommand(tuiSubcommand())
	root.AddCommand(compactSubcommand())
	root.AddCommand(seedSubcommand(v))
	root.AddCommand(runSubcommand())
	root.AddCommand(recordSubcommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runConfig holds all resolved runtime parameters.
type runConfig struct {
	Port                  int
	Dev                   bool
	DBPath                string
	NoBrowser             bool
	RetentionDays         int
	MaxSessions           int
	MaxDBSizeMB           int
	ForwardURLs           []string
	ForwardSample         float64
	RoutesFile            string
	OTLPGRPCPort          int
	OTLPHTTPPort          int
	BindAddressV4         string
	BindAddressV6         string
	TLSCert               string
	TLSKey                string
	BearerToken           string
	SampleRate            int     // 0 = unlimited
	SampleAlwaysKeep      string  // comma list: error,n_plus_one,slow
	SourceRPS             float64 // per-source rate limit; 0 = unlimited
	SourceBurst           int     // per-source burst; 0 = RPS*5
	ForwardSpoolDir       string
	ForwardMaxSpoolMB     int
	ForwardRetryMax       time.Duration
	SelfTelemetryEndpoint string
	SelfTelemetryService  string
	SelfTelemetryInsecure bool
	SelfMonitor           bool
	Debug                 bool
	MCPEnabled            bool
	MCPAllowWrites        bool
	Viper                 *viper.Viper
}

// resolveConfig merges viper config with any explicitly-set CLI flags.
// CLI flags win if they were actually changed from their zero-value sentinel.
func resolveConfig(v *viper.Viper, cmd *cobra.Command, port int, dev bool, dbPath string, noBrowser bool, retentionDays, maxSessions, maxDBSizeMB int, forwardURLs []string, tlsCert, tlsKey, bearerToken string) runConfig {
	cfg := runConfig{
		Port:                  v.GetInt("port"),
		Dev:                   dev,
		DBPath:                expandHome(v.GetString("db_path")),
		NoBrowser:             v.GetBool("no_browser"),
		RetentionDays:         v.GetInt("retention_days"),
		MaxSessions:           v.GetInt("max_sessions"),
		MaxDBSizeMB:           v.GetInt("max_db_size_mb"),
		ForwardURLs:           v.GetStringSlice("forward"),
		ForwardSample:         v.GetFloat64("forward_sample"),
		ForwardSpoolDir:       v.GetString("forward_spool_dir"),
		ForwardMaxSpoolMB:     v.GetInt("forward_max_spool_mb"),
		ForwardRetryMax:       v.GetDuration("forward_retry_max"),
		OTLPGRPCPort:          v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort:          v.GetInt("otlp_http_port"),
		BindAddressV4:         v.GetString("bind_address_v4"),
		BindAddressV6:         v.GetString("bind_address_v6"),
		TLSCert:               v.GetString("tls_cert"),
		TLSKey:                v.GetString("tls_key"),
		BearerToken:           v.GetString("bearer_token"),
		SampleRate:            v.GetInt("sample_rate"),
		SampleAlwaysKeep:      v.GetString("sample_always_keep"),
		SourceRPS:             v.GetFloat64("source_rps"),
		SourceBurst:           v.GetInt("source_burst"),
		SelfTelemetryEndpoint: v.GetString("self_telemetry_endpoint"),
		SelfTelemetryService:  v.GetString("self_telemetry_service"),
		SelfTelemetryInsecure: v.GetBool("self_telemetry_insecure"),
		SelfMonitor:           v.GetBool("self_monitor"),
		MCPEnabled:            v.GetBool("mcp_enabled"),
		MCPAllowWrites:        v.GetBool("mcp_allow_writes"),
	}
	// CLI flags override if explicitly set (non-zero sentinel)
	if f := cmd.Flags().Lookup("port"); f != nil && f.Changed {
		cfg.Port = port
	}
	if f := cmd.Flags().Lookup("no-browser"); f != nil && f.Changed {
		cfg.NoBrowser = noBrowser
	}
	if f := cmd.PersistentFlags().Lookup("db-path"); f != nil && f.Changed {
		cfg.DBPath = expandHome(dbPath)
	}
	if f := cmd.PersistentFlags().Lookup("retention"); f != nil && f.Changed {
		cfg.RetentionDays = retentionDays
	}
	if f := cmd.PersistentFlags().Lookup("max-sessions"); f != nil && f.Changed {
		cfg.MaxSessions = maxSessions
	}
	if f := cmd.PersistentFlags().Lookup("max-db-size"); f != nil && f.Changed {
		cfg.MaxDBSizeMB = maxDBSizeMB
	}
	if f := cmd.Flags().Lookup("forward"); f != nil && f.Changed {
		cfg.ForwardURLs = forwardURLs
	}
	if f := cmd.Flags().Lookup("forward-spool-dir"); f != nil && f.Changed {
		cfg.ForwardSpoolDir = f.Value.String()
	}
	if f := cmd.Flags().Lookup("forward-max-spool-mb"); f != nil && f.Changed {
		if n, err := strconv.Atoi(f.Value.String()); err == nil {
			cfg.ForwardMaxSpoolMB = n
		}
	}
	if f := cmd.Flags().Lookup("forward-retry-max"); f != nil && f.Changed {
		if d, err := time.ParseDuration(f.Value.String()); err == nil {
			cfg.ForwardRetryMax = d
		}
	}
	if f := cmd.Flags().Lookup("tls-cert"); f != nil && f.Changed {
		cfg.TLSCert = tlsCert
	}
	if f := cmd.Flags().Lookup("tls-key"); f != nil && f.Changed {
		cfg.TLSKey = tlsKey
	}
	if f := cmd.Flags().Lookup("bearer-token"); f != nil && f.Changed {
		cfg.BearerToken = bearerToken
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath()
	}
	// OTLP ports: 0 means "disabled" (not "use default"). The viper defaults
	// (4317/4318) already supply the on-by-default behavior, so an explicit 0 in
	// config/env/flags genuinely turns a receiver off.
	if cfg.BindAddressV4 == "" && cfg.BindAddressV6 == "" {
		cfg.BindAddressV4 = "127.0.0.1"
		cfg.BindAddressV6 = "::1"
	}
	if cfg.ForwardSample <= 0 || cfg.ForwardSample > 1 {
		cfg.ForwardSample = 1.0
	}
	// self_monitor auto-wires self-telemetry to Spaniel's own OTLP gRPC port.
	// An explicit self_telemetry_endpoint takes precedence.
	if cfg.SelfMonitor && cfg.SelfTelemetryEndpoint == "" && cfg.OTLPGRPCPort > 0 {
		cfg.SelfTelemetryEndpoint = fmt.Sprintf("localhost:%d", cfg.OTLPGRPCPort)
	}
	cfg.Viper = v
	return cfg
}

func expandHome(p string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// receiverConfigError reports why telemetry could never be ingested with the
// given OTLP receiver ports (both disabled), or nil if at least one is enabled.
// A live OTLP receiver is the only ingest path, so run() refuses to start
// without one rather than masquerade as a viewer that can never receive data.
func receiverConfigError(grpcPort, httpPort int) error {
	if grpcPort == 0 && httpPort == 0 {
		return fmt.Errorf("no OTLP receiver enabled (otlp_grpc_port and otlp_http_port are both 0) — spaniel needs at least one live ingest path; enable one in %s or via SPANIEL_OTLP_GRPC_PORT / SPANIEL_OTLP_HTTP_PORT", globalConfigPath())
	}
	return nil
}

func run(cfg runConfig) error {
	// Fail fast before any side effects (DB open, goroutines) if there's no way
	// to ingest telemetry.
	if err := receiverConfigError(cfg.OTLPGRPCPort, cfg.OTLPHTTPPort); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// otelState holds the currently-active OTel shutdown function under a mutex
	// so it can be hot-swapped when self_monitor is toggled at runtime.
	var otelMu sync.Mutex
	var otelActive func(context.Context) error

	setupOTel := func(endpoint string) error {
		sd, err := telemetry.Setup(context.Background(), telemetry.Config{
			Endpoint:    endpoint,
			ServiceName: cfg.SelfTelemetryService,
			Version:     version,
			DBPath:      cfg.DBPath,
			Insecure:    cfg.SelfTelemetryInsecure,
		})
		if err != nil {
			return err
		}
		otelMu.Lock()
		if otelActive != nil {
			_ = otelActive(context.Background())
		}
		otelActive = sd
		otelMu.Unlock()
		return nil
	}
	// Initialize with empty endpoint; will set up real telemetry after OTLP receivers are running
	if err := setupOTel(""); err != nil {
		return fmt.Errorf("self-telemetry setup: %w", err)
	}
	defer func() {
		otelMu.Lock()
		fn := otelActive
		otelMu.Unlock()
		if fn != nil {
			_ = fn(context.Background())
		}
	}()

	startCtx, startSpan := otel.Tracer("spaniel").Start(context.Background(), "spaniel.startup")

	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close() //nolint:errcheck
	_ = store.SetSpanielVersion(version)

	sess, err := store.CreateSession(time.Now().Format("session_2006-01-02_15:04"), false)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	store.SetActiveSession(sess.ID, sess.Label)

	// Panic-safe: a bare `go` here means a single panic silently kills
	// auto-pruning for the rest of the process lifetime and the disk fills up.
	goroutine.Go(func() {
		runRetention(store, retentionConfig(cfg.RetentionDays, cfg.MaxSessions, cfg.MaxDBSizeMB), sess.ID)
	}, "subsystem", "retention")

	hub := ws.NewHub()
	sampler := ingestion.NewSampler(cfg.SampleRate, ingestion.ParseAlwaysKeep(cfg.SampleAlwaysKeep))
	limiter := ingestion.NewSourceLimiter(cfg.SourceRPS, cfg.SourceBurst)
	pipeline := ingestion.NewPipelineFull(store, hub, sampler, limiter)

	// Seed drop counters from last run.
	if dSpans, dLogs, dMetrics, err := store.LoadDropCounters(); err == nil {
		pipeline.DropCounters().Seed(dSpans, dLogs, dMetrics)
	}
	// Persist drop counters every 30 s.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			dc := pipeline.DropCounters()
			_ = store.SaveDropCounters(dc.DroppedSpansTotal(), dc.DroppedLogsTotal(), dc.DroppedMetricPointsTotal())
		}
	}()

	var fwd *forwarder.Forwarder
	if len(cfg.ForwardURLs) > 0 {
		sc := forwarder.SpoolConfig{
			Dir:      cfg.ForwardSpoolDir,
			MaxBytes: int64(cfg.ForwardMaxSpoolMB) << 20,
			RetryMax: cfg.ForwardRetryMax,
		}
		fwd = forwarder.NewWithSpool(cfg.ForwardURLs, cfg.ForwardSample, sc)
	}

	// A half-configured TLS pair is almost always a mistake that would
	// silently leave the server plaintext — refuse to start instead.
	if (cfg.TLSCert == "") != (cfg.TLSKey == "") {
		set, missing := "--tls-cert", "--tls-key"
		if cfg.TLSKey != "" {
			set, missing = "--tls-key", "--tls-cert"
		}
		return fmt.Errorf("TLS misconfigured: %s is set but %s is missing — both are required to enable TLS", set, missing)
	}
	// Load TLS if both cert and key are configured.
	var tlsCfg *tls.Config
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("load TLS cert/key: %w", err)
		}
		tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	hosts := bindHosts(cfg)

	// Build gRPC server options (TLS and/or bearer auth).
	var grpcOpts []grpc.ServerOption
	if tlsCfg != nil {
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}
	if cfg.BearerToken != "" {
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(receiver.UnaryBearerInterceptor(cfg.BearerToken)))
	}

	grpcRcv := receiver.NewGRPCReceiver(pipeline, grpcOpts...)
	// otlpLs tracks the currently-bound OTLP listeners so the settings
	// endpoint can hot-swap ports without a process restart.
	type otlpLs struct {
		mu   sync.Mutex
		lns  []net.Listener
		port int
	}
	closeOTLP := func(ls *otlpLs) {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		for _, ln := range ls.lns {
			ln.Close()
		}
		ls.lns = nil
		ls.port = 0
	}

	grpcLS := &otlpLs{}
	startGRPC := func(port int) error {
		closeOTLP(grpcLS)
		if port == 0 {
			return nil
		}
		lns, err := listenAll(hosts, port)
		if err != nil {
			return err
		}
		grpcLS.mu.Lock()
		grpcLS.lns = lns
		grpcLS.port = port
		grpcLS.mu.Unlock()
		for _, ln := range lns {
			ln := ln
			go func() {
				if err := grpcRcv.Serve(ln); err != nil {
					slog.Error("grpc receiver stopped", "err", err)
				}
			}()
		}
		return nil
	}
	// Track per-receiver bind success so we can fail fast if none of the
	// configured receivers come up — otherwise the process keeps running, looks
	// healthy, and silently ingests nothing. (The both-disabled case is already
	// rejected at the top of run() via receiverConfigError.)
	rcvUp := 0
	if err := startGRPC(cfg.OTLPGRPCPort); err != nil {
		slog.Error("grpc receiver listen failed", "err", err)
	} else if cfg.OTLPGRPCPort != 0 {
		rcvUp++
	}

	// Now that gRPC receiver is running, set up real telemetry if needed. An
	// explicitly configured endpoint that fails to set up is a hard error — a
	// silent warning here means self-telemetry is misconfigured but invisible.
	if cfg.SelfTelemetryEndpoint != "" {
		if err := setupOTel(cfg.SelfTelemetryEndpoint); err != nil {
			return fmt.Errorf("self-telemetry setup (endpoint %q): %w", cfg.SelfTelemetryEndpoint, err)
		}
	}

	httpRcv := receiver.NewHTTPReceiver(pipeline)
	httpRcv.SetForwarder(fwd)
	otlpMux := http.NewServeMux()
	otlpMux.HandleFunc("/v1/traces", httpRcv.HandleTraces)
	otlpMux.HandleFunc("/v1/logs", httpRcv.HandleLogs)
	otlpMux.HandleFunc("/v1/metrics", httpRcv.HandleMetrics)
	// Instrument inbound OTLP/HTTP so an ingest shows as one trace:
	// "POST /v1/traces" → IngestTraces → db.flush.
	var otlpHandler http.Handler = otelhttp.NewHandler(otlpMux, "spaniel.otlp",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	if cfg.BearerToken != "" {
		otlpHandler = receiver.BearerMiddleware(cfg.BearerToken)(otlpHandler)
	}

	httpLS := &otlpLs{}
	startOTLPHTTP := func(port int) error {
		closeOTLP(httpLS)
		if port == 0 {
			return nil
		}
		lns, err := listenAll(hosts, port)
		if err != nil {
			return err
		}
		lns = wrapTLS(lns, tlsCfg)
		httpLS.mu.Lock()
		httpLS.lns = lns
		httpLS.port = port
		httpLS.mu.Unlock()
		go func() {
			if err := serveHTTP(lns, otlpHandler); err != nil {
				slog.Error("otlp http receiver stopped", "err", err)
			}
		}()
		return nil
	}
	if err := startOTLPHTTP(cfg.OTLPHTTPPort); err != nil {
		slog.Error("otlp http receiver listen failed", "err", err)
	} else if cfg.OTLPHTTPPort != 0 {
		rcvUp++
	}

	// If every configured OTLP receiver failed to bind, there is no way to
	// ingest telemetry — refuse to start rather than masquerade as healthy.
	if rcvUp == 0 {
		return fmt.Errorf("all OTLP receivers failed to bind (gRPC :%d, HTTP :%d) — no telemetry can be ingested; check the ports are free (lsof -i :%d)",
			cfg.OTLPGRPCPort, cfg.OTLPHTTPPort, cfg.OTLPGRPCPort)
	}

	var manifests *coverage.Manifests
	if cfg.RoutesFile != "" {
		m, err := coverage.LoadManifest(cfg.RoutesFile)
		if err != nil {
			slog.Error("routes-file load failed", "err", err)
		} else {
			manifests = m
			total := 0
			for _, rs := range m.Routes {
				total += len(rs)
			}
			fmt.Printf("  Routes    →  %s (%d declared)\n", cfg.RoutesFile, total)
		}
	}
	settingsSvc := &api.SettingsService{
		Viper:          cfg.Viper,
		ConfigPath:     globalConfigPath(),
		Version:        version,
		StartedAt:      time.Now(),
		OTLPGRPCPort:   cfg.OTLPGRPCPort,
		OTLPHTTPPort:   cfg.OTLPHTTPPort,
		TLSEnabled:     tlsCfg != nil,
		BearerTokenSet: cfg.BearerToken != "",
		MCPEnabled:     cfg.MCPEnabled,
		MCPAllowWrites: cfg.MCPAllowWrites,
		LiveGRPCPort: func() int {
			grpcLS.mu.Lock()
			defer grpcLS.mu.Unlock()
			return grpcLS.port
		},
		LiveHTTPPort: func() int {
			httpLS.mu.Lock()
			defer httpLS.mu.Unlock()
			return httpLS.port
		},
		SetLiveGRPCPort: startGRPC,
		SetLiveHTTPPort: startOTLPHTTP,
		SetSelfMonitor: func(enabled bool) error {
			var endpoint string
			if enabled {
				grpcLS.mu.Lock()
				port := grpcLS.port
				grpcLS.mu.Unlock()
				if port <= 0 {
					port = cfg.OTLPGRPCPort
				}
				endpoint = fmt.Sprintf("localhost:%d", port)
			}
			return setupOTel(endpoint)
		},
	}
	apiRouter := api.NewRouterFull(store, hub, fwd, manifests, settingsSvc, pipeline, pipeline.DropCounters(), pipeline)

	var uiHandler http.Handler
	if cfg.Dev {
		viteURL, _ := url.Parse("http://localhost:5173")
		proxy := httputil.NewSingleHostReverseProxy(viteURL)
		// Friendly error when vite isn't up — the default reverse-proxy
		// behaviour returns 502 with no body, which is confusing in
		// `--dev` mode where a missing vite is the most common mistake.
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "spaniel --dev: could not reach vite at http://localhost:5173\n\n"+
				"start it in another terminal:\n  cd frontend && pnpm dev --host 127.0.0.1\n\n"+
				"or run the bundled `make dev` which launches both.\n\nupstream error: %v\n", err)
		}
		uiHandler = proxy
	} else {
		distFS, err := fs.Sub(frontend.DistFS, "dist")
		if err != nil {
			return fmt.Errorf("frontend FS: %w", err)
		}
		// SPA fallback: serve index.html for client-side routes
		uiHandler = &spaHandler{distFS: distFS}
	}

	mainMux := http.NewServeMux()
	mainMux.Handle("/api/", apiRouter)
	mainMux.Handle("/ws", apiRouter)
	if cfg.MCPEnabled {
		mcpHandler := mcp.NewHandler(store, mcp.Options{
			Version:     version,
			AllowWrites: cfg.MCPAllowWrites,
		})
		mainMux.Handle("/mcp", mcpHandler)
		mainMux.Handle("/mcp/", mcpHandler)
	}
	if cfg.Debug {
		mainMux.HandleFunc("/debug/pprof/", pprof.Index)
		mainMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mainMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mainMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mainMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	mainMux.Handle("/", uiHandler)

	var mainHandler http.Handler = mainMux
	if cfg.BearerToken != "" {
		mainHandler = receiver.APIBearerMiddleware(cfg.BearerToken)(mainHandler)
	}

	startSpan.End()
	_ = startCtx
	printBanner(cfg)

	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	if !cfg.NoBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(fmt.Sprintf("%s://localhost:%d", scheme, cfg.Port))
		}()
	}

	// Throughput + forwarder status broadcaster (every 2s)
	go func() {
		var prevSpans, prevLogs int
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stats, err := store.GetStats("")
			if err == nil {
				spansRate := float64(stats.SpanCount-prevSpans) / 2.0
				logsRate := float64(stats.LogCount-prevLogs) / 2.0
				if spansRate < 0 {
					spansRate = 0
				}
				if logsRate < 0 {
					logsRate = 0
				}
				prevSpans = stats.SpanCount
				prevLogs = stats.LogCount
				hub.Broadcast(ws.NewThroughputEvent(&ws.ThroughputPayload{
					SpansPerSec: spansRate,
					LogsPerSec:  logsRate,
				}))
			}
			if fwd != nil {
				for _, s := range fwd.Status() {
					hub.Broadcast(ws.NewForwarderEvent(&ws.ForwarderPayload{
						URL:          s.URL,
						Sent:         s.Sent,
						Errors:       s.Errors,
						LastErr:      s.LastErr,
						PendingBytes: s.PendingBytes,
						DroppedSpool: s.DroppedSpool,
					}))
				}
			}
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	lns, err := listenAll(hosts, cfg.Port)
	if err != nil {
		return fmt.Errorf("ui/api listen: %w", err)
	}

	srv := &http.Server{
		Handler: mainHandler,
		// ReadHeaderTimeout bounds how long a client may take to send request
		// headers — the standard slowloris mitigation. IdleTimeout reaps idle
		// keep-alive connections.
		//
		// ReadTimeout/WriteTimeout are deliberately left unset: this server also
		// carries the long-lived WebSocket (/ws) and long-running pprof profiles
		// (/debug/pprof/profile), which a whole-request deadline would sever. OTLP
		// ingest runs on a separate listener (see startOTLPHTTP), so these
		// timeouts never touch ingest payloads.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// On signal: trigger graceful HTTP drain, then run shutdown tasks.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	serveErr := serveAll(wrapTLS(lns, tlsCfg), srv)

	// Shutdown tasks: run a final retention pass, flush drop counters, close DB.
	retCfg := retentionConfig(cfg.RetentionDays, cfg.MaxSessions, cfg.MaxDBSizeMB)
	if _, err := store.Prune(retCfg, store.ActiveSessionID()); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown retention: %v\n", err)
	}
	dc := pipeline.DropCounters()
	_ = store.SaveDropCounters(dc.DroppedSpansTotal(), dc.DroppedLogsTotal(), dc.DroppedMetricPointsTotal())

	// A listener-closed error from a clean shutdown is not a real failure.
	if ctx.Err() != nil {
		return nil
	}
	return serveErr
}

// bindHosts returns the configured bind addresses (IPv4 and/or IPv6). Blank
// entries are skipped, so a family left empty is simply not served.
func bindHosts(cfg runConfig) []string {
	var hosts []string
	if cfg.BindAddressV4 != "" {
		hosts = append(hosts, cfg.BindAddressV4)
	}
	if cfg.BindAddressV6 != "" {
		hosts = append(hosts, cfg.BindAddressV6)
	}
	return hosts
}

// listenAll binds a TCP listener for each host at the given port. It tolerates
// per-host failures (e.g. an IPv6 wildcard that already covers IPv4) as long as
// at least one listener comes up.
func listenAll(hosts []string, port int) ([]net.Listener, error) {
	var lns []net.Listener
	var firstErr error
	for _, h := range hosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, strconv.Itoa(port)))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("no bind addresses configured")
		}
		// A port already in use is the overwhelmingly common failure here and
		// the bare OS string ("bind: address already in use") gives no next
		// step — point at the likely cause and how to find the culprit.
		if errors.Is(firstErr, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("port %d already in use — another spaniel may be running (lsof -i :%d): %w", port, port, firstErr)
		}
		return nil, firstErr
	}
	return lns, nil
}

func wrapTLS(lns []net.Listener, tlsCfg *tls.Config) []net.Listener {
	if tlsCfg == nil {
		return lns
	}
	wrapped := make([]net.Listener, len(lns))
	for i, ln := range lns {
		wrapped[i] = tls.NewListener(ln, tlsCfg)
	}
	return wrapped
}

// serveHTTP serves h on every listener, returning the first error once all
// listeners have stopped (they are closed together on shutdown / port swap).
func serveHTTP(lns []net.Listener, h http.Handler) error {
	var g errgroup.Group
	for _, ln := range lns {
		g.Go(func() error { return http.Serve(ln, h) })
	}
	return g.Wait()
}

// serveAll serves srv on every listener, returning the first error once all
// listeners have stopped. This is the graceful-shutdown variant: callers drive
// shutdown via srv.Shutdown(ctx) rather than closing listeners directly, so
// the expected http.ErrServerClosed is treated as a clean stop.
func serveAll(lns []net.Listener, srv *http.Server) error {
	var g errgroup.Group
	for _, ln := range lns {
		g.Go(func() error {
			if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		})
	}
	return g.Wait()
}

func printBanner(cfg runConfig) {
	scheme := "http"
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		scheme = "https"
	}
	fmt.Printf("\nspaniel 🐕\n")
	if cfg.Dev {
		fmt.Printf("  UI        →  %s://localhost:%d  (DEV — proxying to vite at :5173)\n", scheme, cfg.Port)
	} else {
		fmt.Printf("  UI        →  %s://localhost:%d\n", scheme, cfg.Port)
	}
	if cfg.OTLPGRPCPort > 0 {
		fmt.Printf("  OTLP/gRPC →  localhost:%d\n", cfg.OTLPGRPCPort)
	} else {
		fmt.Printf("  OTLP/gRPC →  disabled\n")
	}
	if cfg.OTLPHTTPPort > 0 {
		fmt.Printf("  OTLP/HTTP →  localhost:%d\n", cfg.OTLPHTTPPort)
	} else {
		fmt.Printf("  OTLP/HTTP →  disabled\n")
	}
	if cfg.TLSCert != "" {
		fmt.Printf("  TLS       →  enabled (%s)\n", cfg.TLSCert)
	}
	if cfg.BearerToken != "" {
		fmt.Printf("  Auth      →  bearer token required\n")
	}
	if cfg.MCPEnabled {
		writes := "read-only"
		if cfg.MCPAllowWrites {
			writes = "read+write"
		}
		fmt.Printf("  MCP       →  %s://%s:%d/mcp  (%s)\n", scheme, displayHost(cfg), cfg.Port, writes)
	}
	fmt.Printf("  DB        →  %s\n", cfg.DBPath)
	for _, u := range cfg.ForwardURLs {
		fmt.Printf("  Forward   →  %s\n", u)
	}
	fmt.Println()
}

// displayHost returns the host to print in URLs. Loopback and wildcard binds
// resolve to "localhost" (the address a local client uses); an explicit
// non-loopback bind (e.g. a LAN IP) is shown as-is, since "localhost" would be
// wrong for reaching it.
func displayHost(cfg runConfig) string {
	h := cfg.BindAddressV4
	if h == "" {
		h = cfg.BindAddressV6
	}
	switch h {
	case "", "0.0.0.0", "::", "127.0.0.1", "::1":
		return "localhost"
	default:
		return h
	}
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".spaniel", "spaniel.duckdb")
}

func sessionImport(apiBase, label, filePath, format string) error {
	var data []byte
	var err error
	if filePath == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(filePath)
	}
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	q := url.Values{}
	q.Set("label", label)
	q.Set("format", format)
	endpoint := apiBase + "/api/sessions/import?" + q.Encode()

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(data)) //nolint:gosec
	if err != nil {
		return fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("import failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Session struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"session"`
			SpanCount  int `json:"span_count"`
			TraceCount int `json:"trace_count"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck

	d := result.Data
	fmt.Printf("imported: %s (%s) — %d traces, %d spans\n",
		d.Session.Label, d.Session.ID[:8], d.TraceCount, d.SpanCount)
	return nil
}

func sessionNew(apiBase, label string) error {
	body, _ := json.Marshal(map[string]string{"label": label})
	resp, err := http.Post(apiBase+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close()
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result) //nolint:errcheck

	sessID := result.Data.ID
	if sessID == "" {
		return fmt.Errorf("no session ID in response")
	}

	actResp, err := http.Post(apiBase+"/api/sessions/"+sessID+"/activate", "application/json", nil)
	if err != nil {
		return fmt.Errorf("activate session: %w", err)
	}
	actResp.Body.Close()

	fmt.Printf("session created and activated: %s (%s)\n", label, sessID[:8])
	return nil
}

func openBrowser(u string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{u}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", u}
	default:
		cmd = "xdg-open"
		args = []string{u}
	}
	exec.Command(cmd, args...).Start() //nolint:errcheck
}
