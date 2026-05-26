package main

import (
	"context"
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
		port      int
		dev       bool
		dbPath    string
		noBrowser bool
	)

	root := &cobra.Command{
		Use:   "spaniel",
		Short: "Local OpenTelemetry collector and viewer",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(port, dev, dbPath, noBrowser)
		},
	}

	root.Flags().IntVar(&port, "port", 8080, "HTTP server port")
	root.Flags().BoolVar(&dev, "dev", false, "Proxy UI to Vite dev server on :5173")
	root.Flags().StringVar(&dbPath, "db-path", defaultDBPath(), "Path to DuckDB file")
	root.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open browser on startup")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(port int, dev bool, dbPath string, noBrowser bool) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	sess, err := store.CreateSession("")
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	store.SetActiveSession(sess.ID, sess.Label)

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
