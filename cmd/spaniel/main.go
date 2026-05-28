package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/zfogg/spaniel/frontend"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/coverage"
	"github.com/zfogg/spaniel/internal/forwarder"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/receiver"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

// version is overridden at release-build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	v := viper.New()

	var (
		// flag variables — overrides over config file values when explicitly set
		port          int
		dev           bool
		dbPath        string
		noBrowser     bool
		apiBase       string
		retentionDays int
		maxSessions   int
		maxDBSizeMB   int
		forwardURLs   []string
		routesFile    string
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
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs)
			cfg.RoutesFile = routesFile
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
	root.Flags().StringVar(&routesFile, "routes-file", "", "OpenAPI/proto spec file used as the coverage denominator")

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
		Short: "List all sessions (active is marked with *)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sessionList(apiBase, cmd.OutOrStdout())
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
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs)
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
			cfg := resolveConfig(v, cmd, port, dev, dbPath, noBrowser, retentionDays, maxSessions, maxDBSizeMB, forwardURLs)
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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runConfig holds all resolved runtime parameters.
type runConfig struct {
	Port          int
	Dev           bool
	DBPath        string
	NoBrowser     bool
	RetentionDays int
	MaxSessions   int
	MaxDBSizeMB   int
	ForwardURLs   []string
	ForwardSample float64
	RoutesFile    string
	OTLPGRPCPort  int
	OTLPHTTPPort  int
	BindAddress   string
	Viper         *viper.Viper
}

// resolveConfig merges viper config with any explicitly-set CLI flags.
// CLI flags win if they were actually changed from their zero-value sentinel.
func resolveConfig(v *viper.Viper, cmd *cobra.Command, port int, dev bool, dbPath string, noBrowser bool, retentionDays, maxSessions, maxDBSizeMB int, forwardURLs []string) runConfig {
	cfg := runConfig{
		Port:          v.GetInt("port"),
		Dev:           dev,
		DBPath:        expandHome(v.GetString("db_path")),
		NoBrowser:     v.GetBool("no_browser"),
		RetentionDays: v.GetInt("retention_days"),
		MaxSessions:   v.GetInt("max_sessions"),
		MaxDBSizeMB:   v.GetInt("max_db_size_mb"),
		ForwardURLs:   v.GetStringSlice("forward"),
		ForwardSample: v.GetFloat64("forward_sample"),
		OTLPGRPCPort:  v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort:  v.GetInt("otlp_http_port"),
		BindAddress:   v.GetString("bind_address"),
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
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath()
	}
	if cfg.OTLPGRPCPort == 0 {
		cfg.OTLPGRPCPort = 4317
	}
	if cfg.OTLPHTTPPort == 0 {
		cfg.OTLPHTTPPort = 4318
	}
	if cfg.BindAddress == "" {
		cfg.BindAddress = "127.0.0.1"
	}
	if cfg.ForwardSample <= 0 || cfg.ForwardSample > 1 {
		cfg.ForwardSample = 1.0
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

func run(cfg runConfig) error {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close() //nolint:errcheck

	sess, err := store.CreateSession(time.Now().Format("session_2006-01-02_15:04"), false)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	store.SetActiveSession(sess.ID, sess.Label)

	go runRetention(store, retentionConfig(cfg.RetentionDays, cfg.MaxSessions, cfg.MaxDBSizeMB), sess.ID)

	hub := ws.NewHub()
	pipeline := ingestion.NewPipeline(store, hub)

	var fwd *forwarder.Forwarder
	if len(cfg.ForwardURLs) > 0 {
		fwd = forwarder.New(cfg.ForwardURLs, cfg.ForwardSample)
	}

	grpcRcv := receiver.NewGRPCReceiver(pipeline)
	if cfg.OTLPGRPCPort > 0 {
		grpcAddr := fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.OTLPGRPCPort)
		go func() {
			if err := grpcRcv.ListenAndServe(grpcAddr); err != nil {
				fmt.Fprintf(os.Stderr, "grpc receiver: %v\n", err)
			}
		}()
	}

	httpRcv := receiver.NewHTTPReceiver(pipeline)
	httpRcv.SetForwarder(fwd)
	otlpMux := http.NewServeMux()
	otlpMux.HandleFunc("/v1/traces", httpRcv.HandleTraces)
	otlpMux.HandleFunc("/v1/logs", httpRcv.HandleLogs)
	otlpMux.HandleFunc("/v1/metrics", httpRcv.HandleMetrics)
	if cfg.OTLPHTTPPort > 0 {
		httpAddr := fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.OTLPHTTPPort)
		go func() {
			if err := http.ListenAndServe(httpAddr, otlpMux); err != nil {
				fmt.Fprintf(os.Stderr, "otlp http receiver: %v\n", err)
			}
		}()
	}

	var manifests *coverage.Manifests
	if cfg.RoutesFile != "" {
		m, err := coverage.LoadManifest(cfg.RoutesFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "routes-file: %v\n", err)
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
		Viper:      cfg.Viper,
		ConfigPath: globalConfigPath(),
		Version:    version,
		StartedAt:  time.Now(),
		OTLPGRPCPort:   cfg.OTLPGRPCPort,
		OTLPHTTPPort:   cfg.OTLPHTTPPort,
	}
	apiRouter := api.NewRouterFull(store, hub, fwd, manifests, settingsSvc)

	var uiHandler http.Handler
	if cfg.Dev {
		viteURL, _ := url.Parse("http://localhost:5173")
		uiHandler = httputil.NewSingleHostReverseProxy(viteURL)
	} else {
		distFS, err := fs.Sub(frontend.DistFS, "dist")
		if err != nil {
			return fmt.Errorf("frontend FS: %w", err)
		}
		uiHandler = http.FileServer(http.FS(distFS))
	}

	mainMux := http.NewServeMux()
	mainMux.Handle("/api/", apiRouter)
	mainMux.Handle("/ws", apiRouter)
	mainMux.Handle("/", uiHandler)

	printBanner(cfg)

	if !cfg.NoBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://localhost:%d", cfg.Port))
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
						URL:     s.URL,
						Sent:    s.Sent,
						Errors:  s.Errors,
						LastErr: s.LastErr,
					}))
				}
			}
		}
	}()

	_ = context.Background()
	return http.ListenAndServe(fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port), mainMux)
}

func printBanner(cfg runConfig) {
	fmt.Printf("\nspaniel 🐕\n")
	fmt.Printf("  UI        →  http://localhost:%d\n", cfg.Port)
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
	fmt.Printf("  DB        →  %s\n", cfg.DBPath)
	for _, u := range cfg.ForwardURLs {
		fmt.Printf("  Forward   →  %s\n", u)
	}
	fmt.Println()
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
