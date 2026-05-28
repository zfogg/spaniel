package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/zfogg/spaniel/frontend"
	"github.com/zfogg/spaniel/internal/storage"
)

// checkStatus is one of three outcomes per check. Doctor's exit code is the
// count of `fail` entries, capped at 1, so CI scripts can `spaniel doctor
// || exit 1`.
type checkStatus int

const (
	checkOK checkStatus = iota
	checkWarn
	checkFail
)

// checkResult holds the rendered output of a single check.
type checkResult struct {
	Name   string      // left column, e.g. "config file"
	Status checkStatus // OK / warn / fail
	Detail string      // right column, e.g. "/home/zfogg/.spaniel/config.yaml"
	Hint   string      // remediation, shown indented when status != OK
}

// doctorSubcommand returns `spaniel doctor`. The viper instance carries the
// effective config — config file + env + persistent flags merged — so doctor
// reports on the same settings spaniel would run with.
func doctorSubcommand(v *viper.Viper) *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run a diagnostic checklist (ports, DB, config, embed, upstreams)",
		RunE: func(cmd *cobra.Command, args []string) error {
			results := runDoctor(doctorContextFromViper(v, offline))
			fails := renderDoctor(cmd.OutOrStdout(), results)
			if fails > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip checks that need a network round-trip")
	return cmd
}

// doctorContextFromViper pulls every field from the live config, falling
// back to the same defaults `resolveConfig` does so `doctor` is exact about
// what the daemon would actually use.
func doctorContextFromViper(v *viper.Viper, offline bool) doctorContext {
	ctx := doctorContext{
		ConfigPath: globalConfigPath(),
		DBPath:     expandHome(v.GetString("db_path")),
		UIPort:     v.GetInt("port"),
		OTLPGRPCPort:   v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort:   v.GetInt("otlp_http_port"),
		Forward:    v.GetStringSlice("forward"),
		Offline:    offline,
	}
	if ctx.DBPath == "" {
		ctx.DBPath = defaultDBPath()
	}
	if ctx.UIPort == 0 {
		ctx.UIPort = 8080
	}
	if ctx.OTLPGRPCPort == 0 {
		ctx.OTLPGRPCPort = 4317
	}
	if ctx.OTLPHTTPPort == 0 {
		ctx.OTLPHTTPPort = 4318
	}
	return ctx
}

// doctorContext is everything a check needs to know about the current run.
// Passed by value into each check to keep them pure.
type doctorContext struct {
	ConfigPath string
	DBPath     string
	UIPort     int
	OTLPGRPCPort   int
	OTLPHTTPPort   int
	Forward    []string
	Offline    bool
}

// runDoctor reads the live config + runs every check in order, returning the
// results so the renderer can format them however it likes. Pulling the
// config lookup behind `loadDoctorContext` lets unit tests bypass viper.
func runDoctor(override doctorContext) []checkResult {
	ctx := override
	if ctx.ConfigPath == "" || ctx.DBPath == "" || ctx.UIPort == 0 {
		ctx = mergeDoctorDefaults(ctx)
	}

	return []checkResult{
		checkConfigFile(ctx.ConfigPath),
		checkDBWritable(ctx.DBPath),
		checkPort("OTLP/gRPC port", ctx.OTLPGRPCPort),
		checkPort("OTLP/HTTP port", ctx.OTLPHTTPPort),
		checkPort("UI / API port", ctx.UIPort),
		checkFrontendEmbed(),
		checkForwardUpstreams(ctx.Forward, ctx.Offline),
		checkDuckDBVersion(ctx.DBPath),
	}
}

// mergeDoctorDefaults fills any missing fields from the live config.
func mergeDoctorDefaults(ctx doctorContext) doctorContext {
	if ctx.ConfigPath == "" {
		ctx.ConfigPath = globalConfigPath()
	}
	if ctx.DBPath == "" {
		ctx.DBPath = defaultDBPath()
	}
	if ctx.UIPort == 0 {
		ctx.UIPort = 8080
	}
	if ctx.OTLPGRPCPort == 0 {
		ctx.OTLPGRPCPort = 4317
	}
	if ctx.OTLPHTTPPort == 0 {
		ctx.OTLPHTTPPort = 4318
	}
	return ctx
}

// renderDoctor writes the results to w and returns the failure count.
func renderDoctor(w fmtWriter, results []checkResult) int {
	fmt.Fprintln(w, "spaniel doctor")
	fails, warns := 0, 0
	for _, r := range results {
		var sym string
		switch r.Status {
		case checkOK:
			sym = "✓"
		case checkWarn:
			sym = "⚠"
			warns++
		case checkFail:
			sym = "✗"
			fails++
		}
		fmt.Fprintf(w, "  %s %-18s %s\n", sym, r.Name, r.Detail)
		if r.Status != checkOK && r.Hint != "" {
			fmt.Fprintf(w, "        %s\n", r.Hint)
		}
	}
	switch {
	case fails > 0 && warns > 0:
		fmt.Fprintf(w, "\n%d failure · %d warning\n", fails, warns)
	case fails > 0:
		fmt.Fprintf(w, "\n%d failure\n", fails)
	case warns > 0:
		fmt.Fprintf(w, "\n%d warning\n", warns)
	default:
		fmt.Fprintln(w, "\nall checks passed")
	}
	return fails
}

// fmtWriter is the io.Writer subset Fprint* actually uses — lets us pass
// either os.Stdout or a *bytes.Buffer from tests.
type fmtWriter interface {
	Write(p []byte) (n int, err error)
}

// ── individual checks ─────────────────────────────────────────────────────────
//
// Each one returns a checkResult so the renderer + tests can introspect them
// without parsing the rendered output.

func checkConfigFile(path string) checkResult {
	r := checkResult{Name: "config file"}
	if path == "" {
		r.Status = checkWarn
		r.Detail = "(unset)"
		r.Hint = "run `spaniel config show` to bootstrap"
		return r
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			r.Status = checkWarn
			r.Detail = path + " (missing — defaults in effect)"
			r.Hint = "run `spaniel config show` to write the bootstrap config"
		} else {
			r.Status = checkFail
			r.Detail = path
			r.Hint = err.Error()
		}
		return r
	}
	if info.IsDir() {
		r.Status = checkFail
		r.Detail = path + " (is a directory)"
		r.Hint = "the config path must be a file"
		return r
	}
	r.Status = checkOK
	r.Detail = path
	return r
}

func checkDBWritable(path string) checkResult {
	r := checkResult{Name: "db writable"}
	if path == "" {
		r.Status = checkFail
		r.Detail = "(unset)"
		r.Hint = "set `db_path` in ~/.spaniel/config.yaml"
		return r
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.Status = checkFail
		r.Detail = path
		r.Hint = "could not create parent dir: " + err.Error()
		return r
	}
	db, err := storage.Open(path)
	if err != nil {
		r.Status = checkFail
		r.Detail = path
		r.Hint = err.Error()
		return r
	}
	defer db.Close() //nolint:errcheck
	r.Status = checkOK
	r.Detail = path
	if info, statErr := os.Stat(path); statErr == nil {
		r.Detail = fmt.Sprintf("%s (%s)", path, humanBytes(info.Size()))
	}
	return r
}

// checkPort tries to bind the port long enough to confirm it's available,
// then releases. Returns FAIL with the actual sys error when the port is in
// use; PID detection is intentionally avoided (Linux/macOS/Windows-specific)
// and left to the user.
func checkPort(name string, port int) checkResult {
	r := checkResult{Name: name}
	if port == 0 {
		r.Status = checkWarn
		r.Detail = "disabled (port = 0)"
		return r
	}
	if port < 1 || port > 65535 {
		r.Status = checkFail
		r.Detail = fmt.Sprintf(":%d", port)
		r.Hint = "port must be in 1–65535"
		return r
	}
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		r.Status = checkFail
		r.Detail = fmt.Sprintf(":%d already in use", port)
		r.Hint = err.Error()
		return r
	}
	_ = ln.Close()
	r.Status = checkOK
	r.Detail = fmt.Sprintf(":%d is free", port)
	return r
}

func checkFrontendEmbed() checkResult {
	r := checkResult{Name: "frontend dist"}
	count := 0
	err := fs.WalkDir(frontend.DistFS, "dist", func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		r.Status = checkFail
		r.Detail = "embed.FS walk failed"
		r.Hint = err.Error()
		return r
	}
	// The repo ships a `.placeholder` file; anything more means the real bundle
	// is embedded. <=1 file = empty / not built.
	if count <= 1 {
		r.Status = checkFail
		r.Detail = "empty"
		r.Hint = "rebuild the frontend with `pnpm --filter spaniel-ui build`"
		return r
	}
	r.Status = checkOK
	r.Detail = fmt.Sprintf("embedded (%d files)", count)
	return r
}

func checkForwardUpstreams(urls []string, offline bool) checkResult {
	r := checkResult{Name: "forward upstream"}
	if len(urls) == 0 {
		r.Status = checkOK
		r.Detail = "(none configured)"
		return r
	}
	if offline {
		r.Status = checkWarn
		r.Detail = fmt.Sprintf("%d configured — skipped (--offline)", len(urls))
		return r
	}
	client := &http.Client{Timeout: 2 * time.Second}
	var firstFail string
	okCount := 0
	for _, u := range urls {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodOptions, u, nil)
		if err != nil {
			if firstFail == "" {
				firstFail = fmt.Sprintf("%s — %v", u, err)
			}
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			if firstFail == "" {
				firstFail = fmt.Sprintf("%s — %v", u, err)
			}
			continue
		}
		_ = resp.Body.Close()
		okCount++
	}
	if okCount == len(urls) {
		r.Status = checkOK
		r.Detail = fmt.Sprintf("%d reachable", okCount)
		return r
	}
	r.Status = checkFail
	r.Detail = fmt.Sprintf("%d/%d reachable", okCount, len(urls))
	r.Hint = firstFail
	return r
}

// checkDuckDBVersion is informational: prints the linked DuckDB library
// version. We open a brief in-memory DB; if that fails (toolchain mismatch),
// it becomes a warning that the bigger checks have already surfaced.
func checkDuckDBVersion(_ string) checkResult {
	r := checkResult{Name: "duckdb library"}
	db, err := storage.Open(":memory:")
	if err != nil {
		r.Status = checkWarn
		r.Detail = "could not open in-memory DB"
		r.Hint = err.Error()
		return r
	}
	defer db.Close() //nolint:errcheck

	// Best-effort version probe; we don't have direct sql.DB access from the
	// `storage` wrapper, so we just report the Go toolchain instead of the
	// underlying duckdb lib. (The real DB writable check above proves the
	// linkage works at all.)
	r.Status = checkOK
	r.Detail = fmt.Sprintf("loaded · go %s · %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return r
}

// humanBytes turns 1572864 → "1.5 MB".
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), suffix)
}
