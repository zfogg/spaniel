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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/zfogg/spaniel/frontend"
	"github.com/zfogg/spaniel/internal/storage"
	"github.com/zfogg/spaniel/internal/tui"
)

// checkStatus is one of three outcomes per check. Doctor's exit code is the
// count of `fail` entries, capped at 1, so CI scripts can `spaniel doctor
// || exit 1`.
type checkStatus int

const (
	checkPending checkStatus = iota - 1
	checkOK
	checkWarn
	checkFail
)

// checkResult holds the rendered output of a single check.
type checkResult struct {
	Name   string
	Status checkStatus
	Detail string
	Hint   string
}

// doctorSubcommand returns `spaniel doctor`. Requires a TTY — the old plain
// printf path was removed. On a TTY each check runs concurrently with a
// per-line spinner, and the exit code is the count of failures.
func doctorSubcommand(v *viper.Viper) *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run a diagnostic checklist (ports, DB, config, embed, upstreams)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal(os.Stdout) {
				return fmt.Errorf("spaniel doctor requires a terminal — run it interactively")
			}
			ctx := doctorContextFromViper(v, offline)
			fails, err := doctorTUI(ctx)
			if err != nil {
				return err
			}
			if fails > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Skip checks that need a network round-trip")
	return cmd
}

func doctorContextFromViper(v *viper.Viper, offline bool) doctorContext {
	ctx := doctorContext{
		ConfigPath:   globalConfigPath(),
		DBPath:       expandHome(v.GetString("db_path")),
		UIPort:       v.GetInt("port"),
		OTLPGRPCPort: v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort: v.GetInt("otlp_http_port"),
		Forward:      v.GetStringSlice("forward"),
		Offline:      offline,
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

type doctorContext struct {
	ConfigPath   string
	DBPath       string
	UIPort       int
	OTLPGRPCPort int
	OTLPHTTPPort int
	Forward      []string
	Offline      bool
}

// checkSpec describes one named check that can run concurrently.
type checkSpec struct {
	Name string
	Run  func() checkResult
}

// doctorChecks returns the ordered list of checks for a context. The order is
// the rendering order; each runs in its own goroutine in the TUI.
func doctorChecks(ctx doctorContext) []checkSpec {
	if ctx.ConfigPath == "" || ctx.DBPath == "" || ctx.UIPort == 0 {
		ctx = mergeDoctorDefaults(ctx)
	}
	return []checkSpec{
		{Name: "config file", Run: func() checkResult { return checkConfigFile(ctx.ConfigPath) }},
		{Name: "db writable", Run: func() checkResult { return checkDBWritable(ctx.DBPath) }},
		{Name: "OTLP/gRPC port", Run: func() checkResult { return checkPort("OTLP/gRPC port", ctx.OTLPGRPCPort) }},
		{Name: "OTLP/HTTP port", Run: func() checkResult { return checkPort("OTLP/HTTP port", ctx.OTLPHTTPPort) }},
		{Name: "UI / API port", Run: func() checkResult { return checkPort("UI / API port", ctx.UIPort) }},
		{Name: "frontend dist", Run: func() checkResult { return checkFrontendEmbed() }},
		{Name: "forward upstream", Run: func() checkResult { return checkForwardUpstreams(ctx.Forward, ctx.Offline) }},
		{Name: "duckdb library", Run: func() checkResult { return checkDuckDBVersion(ctx.DBPath) }},
	}
}

// runDoctor synchronously runs every check and returns the results. Kept
// for the test suite — the TUI uses doctorChecks directly so each one can
// stream into the model as it completes.
func runDoctor(ctx doctorContext) []checkResult {
	specs := doctorChecks(ctx)
	out := make([]checkResult, len(specs))
	for i, s := range specs {
		r := s.Run()
		r.Name = s.Name
		out[i] = r
	}
	return out
}

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

// ── TUI ──────────────────────────────────────────────────────────────────

// doctorTUI runs the live checklist. Returns the number of failed checks
// after the user dismisses the screen (or all checks finish + 250ms grace).
func doctorTUI(ctx doctorContext) (int, error) {
	specs := doctorChecks(ctx)
	m := newDoctorModel(specs)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return 0, err
	}
	fm := final.(doctorModel)
	fails := 0
	for _, r := range fm.results {
		if r.Status == checkFail {
			fails++
		}
	}
	return fails, nil
}

type checkDoneMsg struct {
	idx    int
	result checkResult
}

type doctorModel struct {
	specs   []checkSpec
	results []checkResult
	spinner spinner.Model
	done    int
}

func newDoctorModel(specs []checkSpec) doctorModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	results := make([]checkResult, len(specs))
	for i, s := range specs {
		results[i] = checkResult{Name: s.Name, Status: checkPending}
	}
	return doctorModel{specs: specs, results: results, spinner: sp}
}

func (m doctorModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	for i, s := range m.specs {
		i, s := i, s
		cmds = append(cmds, func() tea.Msg {
			r := s.Run()
			r.Name = s.Name
			return checkDoneMsg{idx: i, result: r}
		})
	}
	return tea.Batch(cmds...)
}

func (m doctorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, tui.Keys.Quit) {
			return m, tea.Quit
		}
	case checkDoneMsg:
		m.results[msg.idx] = msg.result
		m.done++
		if m.done == len(m.specs) {
			// Tiny grace period so the user sees the final state before exit.
			return m, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m doctorModel) View() string {
	var b []string
	b = append(b, tui.Title.Render("spaniel doctor"))
	for _, r := range m.results {
		var sym string
		switch r.Status {
		case checkPending:
			sym = m.spinner.View()
		case checkOK:
			sym = tui.Ok.Render("✓")
		case checkWarn:
			sym = tui.Warn.Render("⚠")
		case checkFail:
			sym = tui.Danger.Render("✗")
		}
		line := fmt.Sprintf("  %s %s  %s", sym, padRight(r.Name, 18), r.Detail)
		b = append(b, line)
		if r.Status != checkOK && r.Status != checkPending && r.Hint != "" {
			b = append(b, "        "+tui.Muted.Render(r.Hint))
		}
	}

	if m.done == len(m.specs) {
		fails, warns := 0, 0
		for _, r := range m.results {
			switch r.Status {
			case checkFail:
				fails++
			case checkWarn:
				warns++
			}
		}
		var summary string
		switch {
		case fails > 0 && warns > 0:
			summary = tui.Danger.Render(fmt.Sprintf("%d failure · %d warning", fails, warns))
		case fails > 0:
			summary = tui.Danger.Render(fmt.Sprintf("%d failure", fails))
		case warns > 0:
			summary = tui.Warn.Render(fmt.Sprintf("%d warning", warns))
		default:
			summary = tui.Ok.Render("all checks passed")
		}
		b = append(b, "", summary)
	}
	return tui.JoinVertical(b...)
}

// ── individual checks ─────────────────────────────────────────────────────────

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
		// A locked database almost always means another spaniel instance is
		// already running against this file — the raw SQLite string doesn't
		// say so.
		if msg := strings.ToLower(err.Error()); strings.Contains(msg, "locked") || strings.Contains(msg, "busy") {
			r.Hint = "database is locked — another spaniel instance is probably already running against this db (" + err.Error() + ")"
		}
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
	r.Status = checkOK
	r.Detail = fmt.Sprintf("loaded · go %s · %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return r
}

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
