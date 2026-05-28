package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// SettingsService is everything the settings endpoints need that isn't
// already on the Router (storage, hub, etc.). main.go builds one of these
// after viper has been bootstrapped and hands it to Router.SetSettingsService.
type SettingsService struct {
	Viper      *viper.Viper
	ConfigPath string // path to the file Save() will write
	Version    string
	StartedAt  time.Time
	OTLPGRPCPort   int
	OTLPHTTPPort   int
}

// SettingsResponse is the JSON shape returned by GET /api/settings.
// Persistable fields live at the top level; read-only info is under Runtime.
type SettingsResponse struct {
	Port          int      `json:"port"`
	DBPath        string   `json:"db_path"`
	RetentionDays int      `json:"retention_days"`
	MaxSessions   int      `json:"max_sessions"`
	MaxDBSizeMB   int      `json:"max_db_size_mb"`
	OTLPGRPCPort      int      `json:"otlp_grpc_port"`
	OTLPHTTPPort      int      `json:"otlp_http_port"`
	NoBrowser     bool     `json:"no_browser"`
	Forward       []string `json:"forward"`
	BindAddress   string   `json:"bind_address"`
	ForwardSample float64  `json:"forward_sample"`

	Runtime SettingsRuntime `json:"runtime"`
}

// SettingsRuntime is everything the UI shows but never writes back.
type SettingsRuntime struct {
	PID         int    `json:"pid"`
	UptimeNs    int64  `json:"uptime_ns"`
	Version     string `json:"version"`
	ConfigPath  string `json:"config_path"`
	OTLPGRPCPort    int    `json:"otlp_grpc_port"`
	OTLPHTTPPort    int    `json:"otlp_http_port"`
	DBSizeBytes int64  `json:"db_size_bytes"`
}

// SettingsUpdate is the writable subset accepted by PUT /api/settings.
// Pointer fields let clients send partial updates — nil = "leave as-is".
type SettingsUpdate struct {
	Port          *int      `json:"port,omitempty"`
	DBPath        *string   `json:"db_path,omitempty"`
	RetentionDays *int      `json:"retention_days,omitempty"`
	MaxSessions   *int      `json:"max_sessions,omitempty"`
	MaxDBSizeMB   *int      `json:"max_db_size_mb,omitempty"`
	OTLPGRPCPort      *int      `json:"otlp_grpc_port,omitempty"`
	OTLPHTTPPort      *int      `json:"otlp_http_port,omitempty"`
	NoBrowser     *bool     `json:"no_browser,omitempty"`
	Forward       *[]string `json:"forward,omitempty"`
	BindAddress   *string   `json:"bind_address,omitempty"`
	ForwardSample *float64  `json:"forward_sample,omitempty"`
}

func (r *Router) getSettings(w http.ResponseWriter, _ *http.Request) {
	if r.settings == nil {
		respondErr(w, 404, "settings service not configured")
		return
	}
	resp := r.buildSettings()
	respond(w, resp, 1, 1)
}

func (r *Router) putSettings(w http.ResponseWriter, req *http.Request) {
	if r.settings == nil {
		respondErr(w, 404, "settings service not configured")
		return
	}
	var body SettingsUpdate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		respondErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if err := validateSettings(&body); err != nil {
		respondErr(w, 400, err.Error())
		return
	}
	if err := applySettings(r.settings, &body); err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, r.buildSettings(), 1, 1)
}

// dropAllData wipes the database (DELETE /api/settings/data). Distinct from
// the existing /api/sessions endpoints which are per-session.
func (r *Router) dropAllData(w http.ResponseWriter, _ *http.Request) {
	if err := r.store.Reset(); err != nil {
		respondErr(w, 500, err.Error())
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
		Port:          v.GetInt("port"),
		DBPath:        v.GetString("db_path"),
		RetentionDays: v.GetInt("retention_days"),
		MaxSessions:   v.GetInt("max_sessions"),
		MaxDBSizeMB:   v.GetInt("max_db_size_mb"),
		OTLPGRPCPort:      v.GetInt("otlp_grpc_port"),
		OTLPHTTPPort:      v.GetInt("otlp_http_port"),
		NoBrowser:     v.GetBool("no_browser"),
		Forward:       v.GetStringSlice("forward"),
		BindAddress:   v.GetString("bind_address"),
		ForwardSample: v.GetFloat64("forward_sample"),
		Runtime: SettingsRuntime{
			PID:        os.Getpid(),
			UptimeNs:   time.Since(s.StartedAt).Nanoseconds(),
			Version:    s.Version,
			ConfigPath: s.ConfigPath,
			OTLPGRPCPort:   s.OTLPGRPCPort,
			OTLPHTTPPort:   s.OTLPHTTPPort,
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
	if u.BindAddress != nil {
		switch *u.BindAddress {
		case "127.0.0.1", "0.0.0.0", "::1":
		default:
			return fmt.Errorf("bind_address must be 127.0.0.1, 0.0.0.0, or ::1, got %q", *u.BindAddress)
		}
	}
	if u.ForwardSample != nil && (*u.ForwardSample < 0 || *u.ForwardSample > 1) {
		return fmt.Errorf("forward_sample must be between 0 and 1, got %v", *u.ForwardSample)
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
		s.Viper.Set("otlp_grpc_port", *u.OTLPGRPCPort)
	}
	if u.OTLPHTTPPort != nil {
		s.Viper.Set("otlp_http_port", *u.OTLPHTTPPort)
	}
	if u.NoBrowser != nil {
		s.Viper.Set("no_browser", *u.NoBrowser)
	}
	if u.Forward != nil {
		s.Viper.Set("forward", *u.Forward)
	}
	if u.BindAddress != nil {
		s.Viper.Set("bind_address", *u.BindAddress)
	}
	if u.ForwardSample != nil {
		s.Viper.Set("forward_sample", *u.ForwardSample)
	}

	if s.ConfigPath == "" {
		// In-memory only (tests). Nothing to persist.
		return nil
	}
	// Fresh viper to avoid merging project-level config into the global file.
	out := viper.New()
	out.SetConfigFile(s.ConfigPath)
	for _, k := range []string{"port", "db_path", "retention_days", "max_sessions", "max_db_size_mb", "otlp_grpc_port", "otlp_http_port", "no_browser", "forward", "bind_address", "forward_sample"} {
		out.Set(k, s.Viper.Get(k))
	}
	if err := os.MkdirAll(parentDir(s.ConfigPath), 0o750); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	return out.WriteConfig()
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
