package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/zfogg/spaniel/frontend"
	"github.com/zfogg/spaniel/internal/api"
	"github.com/zfogg/spaniel/internal/ingestion"
	"github.com/zfogg/spaniel/internal/receiver"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/ws"
)

func main() {
	var (
		port          int
		dev           bool
		dbPath        string
		noBrowser     bool
		apiBase       string
		retentionDays int
		maxSessions   int
		maxDBSizeMB   int
	)

	root := &cobra.Command{
		Use:   "spaniel",
		Short: "Local OpenTelemetry collector and viewer",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(port, dev, dbPath, noBrowser, retentionConfig(retentionDays, maxSessions, maxDBSizeMB))
		},
	}

	root.PersistentFlags().StringVar(&dbPath, "db-path", defaultDBPath(), "Path to DuckDB file")
	root.PersistentFlags().IntVar(&retentionDays, "retention", 7, "Delete sessions older than N days (0 = disabled)")
	root.PersistentFlags().IntVar(&maxSessions, "max-sessions", 50, "Keep at most N sessions (0 = unlimited)")
	root.PersistentFlags().IntVar(&maxDBSizeMB, "max-db-size", 500, "Shrink DB to at most N MB (0 = unlimited)")
	root.Flags().IntVar(&port, "port", 8080, "HTTP server port")
	root.Flags().BoolVar(&dev, "dev", false, "Proxy UI to Vite dev server on :5173")
	root.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open browser on startup")

	// session subcommand
	sessionCmd := &cobra.Command{
		Use:   "session",
		Short: "Manage spaniel sessions",
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
	root.AddCommand(sessionCmd)

	// prune subcommand: apply retention policy once and exit.
	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Apply the retention policy now (delete old / excess / oversized data)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return prune(dbPath, retentionConfig(retentionDays, maxSessions, maxDBSizeMB))
		},
	}
	root.AddCommand(pruneCmd)

	// reset subcommand: wipe everything.
	var resetYes bool
	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe all spaniel data and start fresh",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !resetYes {
				return fmt.Errorf("refusing to wipe data without --yes")
			}
			return reset(dbPath)
		},
	}
	resetCmd.Flags().BoolVar(&resetYes, "yes", false, "Confirm: yes, delete everything")
	root.AddCommand(resetCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(port int, dev bool, dbPath string, noBrowser bool, retention storage.RetentionConfig) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	sess, err := store.CreateSession(time.Now().Format("session_2006-01-02_15:04"), false)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	store.SetActiveSession(sess.ID, sess.Label)

	// Retention: run once at startup, then every hour. The active session is
	// the one we just created; retention preserves it.
	go runRetention(store, retention, sess.ID)

	hub := ws.NewHub()
	pipeline := ingestion.NewPipeline(store, hub)

	// gRPC OTLP receiver on :4317
	grpcRcv := receiver.NewGRPCReceiver(pipeline)
	go func() {
		if err := grpcRcv.ListenAndServe(":4317"); err != nil {
			fmt.Fprintf(os.Stderr, "grpc receiver: %v\n", err)
		}
	}()

	// HTTP OTLP receiver on :4318
	httpRcv := receiver.NewHTTPReceiver(pipeline)
	otlpMux := http.NewServeMux()
	otlpMux.HandleFunc("/v1/traces", httpRcv.HandleTraces)
	otlpMux.HandleFunc("/v1/logs", httpRcv.HandleLogs)
	otlpMux.HandleFunc("/v1/metrics", httpRcv.HandleMetrics)
	go func() {
		if err := http.ListenAndServe(":4318", otlpMux); err != nil {
			fmt.Fprintf(os.Stderr, "otlp http receiver: %v\n", err)
		}
	}()

	// Main HTTP server (API + UI)
	apiRouter := api.NewRouter(store, hub)

	var uiHandler http.Handler
	if dev {
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

	addr := fmt.Sprintf(":%d", port)

	printBanner(port, dbPath)

	if !noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(fmt.Sprintf("http://localhost:%d", port))
		}()
	}

	_ = context.Background()
	return http.ListenAndServe(addr, mainMux)
}

func printBanner(port int, dbPath string) {
	fmt.Printf("\nspaniel 🐕\n")
	fmt.Printf("  UI        →  http://localhost:%d\n", port)
	fmt.Printf("  OTLP/gRPC →  localhost:4317\n")
	fmt.Printf("  OTLP/HTTP →  localhost:4318\n")
	fmt.Printf("  DB        →  %s\n\n", dbPath)
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".spaniel", "spaniel.duckdb")
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

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	exec.Command(cmd, args...).Start() //nolint:errcheck
}
