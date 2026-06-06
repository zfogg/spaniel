package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// SettingsService is everything the settings endpoints need that isn't
// already on the Router (storage, hub, etc.). main.go builds one of these
// after viper has been bootstrapped and hands it to Router.SetSettingsService.
type SettingsService struct {
	Viper          *viper.Viper
	ConfigPath     string // path to the file Save() will write
	Version        string
	StartedAt      time.Time
	OTLPGRPCPort   int
	OTLPHTTPPort   int
	TLSEnabled     bool
	BearerTokenSet bool
	MCPEnabled     bool
	MCPAllowWrites bool

	// LiveGRPCPort / LiveHTTPPort return the port currently bound (0 = stopped).
	// When nil the startup values above are used (tests / minimal configs).
	LiveGRPCPort func() int
	LiveHTTPPort func() int

	// SetLiveGRPCPort / SetLiveHTTPPort hot-swap the running OTLP listener.
	// Called by applySettings when the port config changes; nil = no-op.
	SetLiveGRPCPort func(int) error
	SetLiveHTTPPort func(int) error

	// SetSelfMonitor enables or disables self-telemetry without a restart.
	// When enabled, Spaniel sends its own traces/metrics to its own OTLP gRPC
	// port. nil = setting is persisted but takes effect on next restart only.
	SetSelfMonitor func(bool) error

	// GithubClient is injected in tests to stub the GitHub API. nil = use
	// http.DefaultClient.
	GithubClient *http.Client

	// update check cache
	updateMu       sync.Mutex
	updateCache    *UpdateCheckResult
	updateCachedAt time.Time
}

// UpdateCheckResult is the JSON shape returned by POST /api/settings/check-updates.
type UpdateCheckResult struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	Channel         string `json:"channel"`
	IsOutdated      bool   `json:"is_outdated"`
	ReleaseNotesURL string `json:"release_notes_url"`
	CheckedAtNs     int64  `json:"checked_at_ns"`
	Error           string `json:"error,omitempty"`
}

// SettingsResponse is the JSON shape returned by GET /api/settings.
// Persistable fields live at the top level; read-only info is under Runtime.
type SettingsResponse struct {
	Port           int      `json:"port"`
	DBPath         string   `json:"db_path"`
	RetentionDays  int      `json:"retention_days"`
	MaxSessions    int      `json:"max_sessions"`
	MaxDBSizeMB    int      `json:"max_db_size_mb"`
	OTLPGRPCPort   int      `json:"otlp_grpc_port"`
	OTLPHTTPPort   int      `json:"otlp_http_port"`
	NoBrowser      bool     `json:"no_browser"`
	Forward        []string `json:"forward"`
	BindAddressV4  string   `json:"bind_address_v4"`
	BindAddressV6  string   `json:"bind_address_v6"`
	ForwardSample  float64  `json:"forward_sample"`
	SourceRPS      float64  `json:"source_rps"`
	SourceBurst    int      `json:"source_burst"`
	TLSEnabled     bool     `json:"tls_enabled"`
	BearerTokenSet bool     `json:"bearer_token_set"`
	SelfMonitor    bool     `json:"self_monitor"`
	MCPEnabled     bool     `json:"mcp_enabled"`
	MCPAllowWrites bool     `json:"mcp_allow_writes"`

	Runtime SettingsRuntime `json:"runtime"`
}

// SettingsRuntime is everything the UI shows but never writes back.
type SettingsRuntime struct {
	PID          int    `json:"pid"`
	UptimeNs     int64  `json:"uptime_ns"`
	Version      string `json:"version"`
	Channel      string `json:"channel"`
	ConfigPath   string `json:"config_path"`
	OTLPGRPCPort int    `json:"otlp_grpc_port"`
	OTLPHTTPPort int    `json:"otlp_http_port"`
	DBSizeBytes  int64  `json:"db_size_bytes"`
}

// SettingsUpdate is the writable subset accepted by PUT /api/settings.
// Pointer fields let clients send partial updates — nil = "leave as-is".
type SettingsUpdate struct {
	Port          *int      `json:"port,omitempty"`
	DBPath        *string   `json:"db_path,omitempty"`
	RetentionDays *int      `json:"retention_days,omitempty"`
	MaxSessions   *int      `json:"max_sessions,omitempty"`
	MaxDBSizeMB   *int      `json:"max_db_size_mb,omitempty"`
	OTLPGRPCPort  *int      `json:"otlp_grpc_port,omitempty"`
	OTLPHTTPPort  *int      `json:"otlp_http_port,omitempty"`
	NoBrowser     *bool     `json:"no_browser,omitempty"`
	Forward       *[]string `json:"forward,omitempty"`
	BindAddressV4 *string   `json:"bind_address_v4,omitempty"`
	BindAddressV6 *string   `json:"bind_address_v6,omitempty"`
	ForwardSample *float64  `json:"forward_sample,omitempty"`
	SourceRPS     *float64  `json:"source_rps,omitempty"`
	SourceBurst   *int      `json:"source_burst,omitempty"`
	SelfMonitor   *bool     `json:"self_monitor,omitempty"`
}

func (r *Router) getSettings(w http.ResponseWriter, req *http.Request) {
	if r.settings == nil {
		respondErr(w, req, 404, "settings service not configured")
		return
	}
	resp := r.buildSettings()
	respond(w, resp, 1, 1)
}

func (r *Router) putSettings(w http.ResponseWriter, req *http.Request) {
	if r.settings == nil {
		respondErr(w, req, 404, "settings service not configured")
		return
	}
	var body SettingsUpdate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondErr(w, req, 400, "invalid JSON: "+err.Error())
		return
	}
	if err := validateSettings(&body); err != nil {
		respondErr(w, req, 400, err.Error())
		return
	}
	if err := applySettings(r.settings, &body); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, r.buildSettings(), 1, 1)
}

// dropAllData wipes the database (DELETE /api/settings/data). Distinct from
// the existing /api/sessions endpoints which are per-session.
func (r *Router) dropAllData(w http.ResponseWriter, req *http.Request) {
	if err := r.store.Reset(); err != nil {
		respondErr(w, req, 500, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

// buildSettings snapshots the current viper + runtime state into the
// response struct. Pure read-side — no I/O beyond os.Getpid / time.Since.
func (r *Router) buildSettings() SettingsResponse {
	s := r.settings
	v := s.Viper

	resp := SettingsResponse{
		Port:           v.GetInt("port"),
		DBPath:         v.GetString("db_path"),
		RetentionDays:  v.GetInt("retention_days"),
		MaxSessions:    v.GetInt("max_sessions"),
		MaxDBSizeMB:    v.GetInt("max_db_size_mb"),
		OTLPGRPCPort:   v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort:   v.GetInt("otlp_http_port"),
		NoBrowser:      v.GetBool("no_browser"),
		Forward:        nonNilStrings(v.GetStringSlice("forward")),
		BindAddressV4:  v.GetString("bind_address_v4"),
		BindAddressV6:  v.GetString("bind_address_v6"),
		ForwardSample:  v.GetFloat64("forward_sample"),
		SourceRPS:      v.GetFloat64("source_rps"),
		SourceBurst:    v.GetInt("source_burst"),
		TLSEnabled:     s.TLSEnabled,
		BearerTokenSet: s.BearerTokenSet,
		SelfMonitor:    v.GetBool("self_monitor"),
		MCPEnabled:     s.MCPEnabled,
		MCPAllowWrites: s.MCPAllowWrites,
		Runtime: SettingsRuntime{
			PID:          os.Getpid(),
			UptimeNs:     time.Since(s.StartedAt).Nanoseconds(),
			Version:      s.Version,
			Channel:      versionChannel(s.Version),
			ConfigPath:   s.ConfigPath,
			OTLPGRPCPort: livePort(s.LiveGRPCPort, s.OTLPGRPCPort),
			OTLPHTTPPort: livePort(s.LiveHTTPPort, s.OTLPHTTPPort),
		},
	}
	// DB size lives on the storage layer.
	stats, _ := r.store.GetStats("")
	if stats != nil {
		resp.Runtime.DBSizeBytes = stats.DBSize
	}
	return resp
}

// validateSettings rejects updates whose values can't possibly be applied.
// We're strict about ports (1–65535) and non-negative integers; permissive
// about everything else (the user's file is their file).
func validateSettings(u *SettingsUpdate) error {
	for label, p := range map[string]*int{"port": u.Port, "otlp_grpc_port": u.OTLPGRPCPort, "otlp_http_port": u.OTLPHTTPPort} {
		if p == nil {
			continue
		}
		// 0 is allowed for grpc_port/http_port (means "disabled"); the main HTTP
		// port (`port`) must be a real port.
		if label == "port" && (*p < 1 || *p > 65535) {
			return fmt.Errorf("%s must be 1–65535, got %d", label, *p)
		}
		if label != "port" && (*p < 0 || *p > 65535) {
			return fmt.Errorf("%s must be 0–65535, got %d", label, *p)
		}
	}
	if u.RetentionDays != nil && *u.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be ≥ 0")
	}
	if u.MaxSessions != nil && *u.MaxSessions < 0 {
		return fmt.Errorf("max_sessions must be ≥ 0")
	}
	if u.MaxDBSizeMB != nil && *u.MaxDBSizeMB < 0 {
		return fmt.Errorf("max_db_size_mb must be ≥ 0")
	}
	if u.DBPath != nil && strings.TrimSpace(*u.DBPath) == "" {
		return fmt.Errorf("db_path cannot be empty")
	}
	if u.Forward != nil {
		for _, url := range *u.Forward {
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				return fmt.Errorf("forward URL must start with http:// or https://: %q", url)
			}
		}
	}
	if u.BindAddressV4 != nil {
		if addr := strings.TrimSpace(*u.BindAddressV4); addr != "" {
			ip := net.ParseIP(addr)
			if ip == nil || ip.To4() == nil {
				return fmt.Errorf("bind_address_v4 must be a valid IPv4 address or empty, got %q", *u.BindAddressV4)
			}
		}
	}
	if u.BindAddressV6 != nil {
		if addr := strings.TrimSpace(*u.BindAddressV6); addr != "" {
			ip := net.ParseIP(addr)
			if ip == nil || ip.To4() != nil {
				return fmt.Errorf("bind_address_v6 must be a valid IPv6 address or empty, got %q", *u.BindAddressV6)
			}
		}
	}
	if u.ForwardSample != nil && (*u.ForwardSample < 0 || *u.ForwardSample > 1) {
		return fmt.Errorf("forward_sample must be between 0 and 1, got %v", *u.ForwardSample)
	}
	if u.SourceRPS != nil && *u.SourceRPS < 0 {
		return fmt.Errorf("source_rps must be ≥ 0")
	}
	if u.SourceBurst != nil && *u.SourceBurst < 0 {
		return fmt.Errorf("source_burst must be ≥ 0")
	}
	return nil
}

// applySettings mutates the live viper and persists to disk. Writing uses a
// fresh viper instance so we don't accidentally bake any project-level
// .spaniel.yaml merges into the global file.
func applySettings(s *SettingsService, u *SettingsUpdate) error {
	if u.Port != nil {
		s.Viper.Set("port", *u.Port)
	}
	if u.DBPath != nil {
		s.Viper.Set("db_path", *u.DBPath)
	}
	if u.RetentionDays != nil {
		s.Viper.Set("retention_days", *u.RetentionDays)
	}
	if u.MaxSessions != nil {
		s.Viper.Set("max_sessions", *u.MaxSessions)
	}
	if u.MaxDBSizeMB != nil {
		s.Viper.Set("max_db_size_mb", *u.MaxDBSizeMB)
	}
	if u.OTLPGRPCPort != nil {
		if s.SetLiveGRPCPort != nil {
			if err := s.SetLiveGRPCPort(*u.OTLPGRPCPort); err != nil {
				return fmt.Errorf("set grpc port %d: %w", *u.OTLPGRPCPort, err)
			}
		}
		s.Viper.Set("otlp_grpc_port", *u.OTLPGRPCPort)
	}
	if u.OTLPHTTPPort != nil {
		if s.SetLiveHTTPPort != nil {
			if err := s.SetLiveHTTPPort(*u.OTLPHTTPPort); err != nil {
				return fmt.Errorf("set http port %d: %w", *u.OTLPHTTPPort, err)
			}
		}
		s.Viper.Set("otlp_http_port", *u.OTLPHTTPPort)
	}
	if u.NoBrowser != nil {
		s.Viper.Set("no_browser", *u.NoBrowser)
	}
	if u.Forward != nil {
		s.Viper.Set("forward", *u.Forward)
	}
	if u.BindAddressV4 != nil {
		s.Viper.Set("bind_address_v4", strings.TrimSpace(*u.BindAddressV4))
	}
	if u.BindAddressV6 != nil {
		s.Viper.Set("bind_address_v6", strings.TrimSpace(*u.BindAddressV6))
	}
	if u.ForwardSample != nil {
		s.Viper.Set("forward_sample", *u.ForwardSample)
	}
	if u.SourceRPS != nil {
		s.Viper.Set("source_rps", *u.SourceRPS)
	}
	if u.SourceBurst != nil {
		s.Viper.Set("source_burst", *u.SourceBurst)
	}
	if u.SelfMonitor != nil {
		s.Viper.Set("self_monitor", *u.SelfMonitor)
		if s.SetSelfMonitor != nil {
			if err := s.SetSelfMonitor(*u.SelfMonitor); err != nil {
				return fmt.Errorf("set self-monitor: %w", err)
			}
		}
	}

	if s.ConfigPath == "" {
		// In-memory only (tests). Nothing to persist.
		return nil
	}
	// Fresh viper to avoid merging project-level config into the global file.
	out := viper.New()
	out.SetConfigFile(s.ConfigPath)
	for _, k := range []string{"port", "db_path", "retention_days", "max_sessions", "max_db_size_mb", "otlp_grpc_port", "otlp_http_port", "no_browser", "forward", "bind_address_v4", "bind_address_v6", "forward_sample", "source_rps", "source_burst", "self_monitor"} {
		out.Set(k, s.Viper.Get(k))
	}
	if err := os.MkdirAll(parentDir(s.ConfigPath), 0o750); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	// Write to a sibling temp file and atomically rename into place so a
	// failed/partial write can never truncate the live config — a torn write
	// would leave on-disk config out of sync with the in-memory state we
	// already mutated above. The temp name keeps the real extension (e.g.
	// config.tmp.yaml) because viper.WriteConfigAs derives the format from it.
	ext := filepath.Ext(s.ConfigPath)
	tmp := strings.TrimSuffix(s.ConfigPath, ext) + ".tmp" + ext
	if err := out.WriteConfigAs(tmp); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, s.ConfigPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit config: %w", err)
	}
	return nil
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// checkUpdates hits the GitHub releases API and returns the latest version.
// Results are cached for 1 hour. Network failures degrade gracefully.
func (r *Router) checkUpdates(w http.ResponseWriter, req *http.Request) {
	if r.settings == nil {
		respondErr(w, req, 404, "settings service not configured")
		return
	}
	s := r.settings
	channel := versionChannel(s.Version)

	// Check cache under lock.
	s.updateMu.Lock()
	if s.updateCache != nil && time.Since(s.updateCachedAt) < time.Hour {
		cached := *s.updateCache
		s.updateMu.Unlock()
		respond(w, cached, 1, 1)
		return
	}
	s.updateMu.Unlock()

	client := s.GithubClient
	if client == nil {
		client = http.DefaultClient
	}

	result := UpdateCheckResult{
		Current:     s.Version,
		Channel:     channel,
		CheckedAtNs: time.Now().UnixNano(),
	}

	resp, err := client.Get("https://api.github.com/repos/zfogg/spaniel/releases/latest")
	if err != nil || resp.StatusCode != http.StatusOK {
		result.Error = "could not reach github"
		respond(w, result, 1, 1)
		return
	}
	defer resp.Body.Close()

	var ghResp struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ghResp); err != nil {
		result.Error = "could not reach github"
		respond(w, result, 1, 1)
		return
	}

	latest := strings.TrimPrefix(ghResp.TagName, "v")
	result.Latest = latest
	result.ReleaseNotesURL = ghResp.HTMLURL
	result.IsOutdated = isNewer(s.Version, latest)

	s.updateMu.Lock()
	s.updateCache = &result
	s.updateCachedAt = time.Now()
	s.updateMu.Unlock()

	respond(w, result, 1, 1)
}

// versionChannel returns "dev" when version == "dev", "stable" otherwise.
func versionChannel(version string) string {
	if version == "dev" {
		return "dev"
	}
	return "stable"
}

// isNewer reports whether latest is a higher semver than current.
// Returns false if either version is not valid semver or if current == "dev".
func isNewer(current, latest string) bool {
	if current == "dev" || latest == "dev" {
		return false
	}
	cp := parseSemver(current)
	lp := parseSemver(latest)
	if cp == nil || lp == nil {
		return false
	}
	for i := range cp {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

// parseSemver splits "major.minor.patch" into [3]int. Returns nil on failure.
func parseSemver(v string) *[3]int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return &out
}

// nonNilStrings ensures the slice is never nil so the JSON encoder emits
// `[]` instead of `null`. Frontend types treat `forward` as a non-optional
// string array; receiving null would crash `.join()`.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// livePort returns the currently active port from the live function if set,
// otherwise falls back to the startup value.
func livePort(live func() int, fallback int) int {
	if live != nil {
		return live()
	}
	return fallback
}
