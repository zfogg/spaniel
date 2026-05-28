package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const defaultConfig = `# spaniel configuration
# CLI flags override these values; project .spaniel.yaml overrides global config.

# HTTP port for the spaniel UI and API
port: 8080

# Path to the DuckDB database file
db_path: ~/.spaniel/spaniel.duckdb

# Retention: delete sessions older than N days (0 = disabled)
retention_days: 7

# Retention: keep at most N sessions (0 = unlimited)
max_sessions: 50

# Retention: shrink DB to at most N MB (0 = unlimited)
max_db_size_mb: 500

# OTLP gRPC receiver port (default 4317; 0 = disabled)
otlp_grpc_port: 4317

# OTLP HTTP receiver port (default 4318; 0 = disabled)
otlp_http_port: 4318

# Do not open a browser tab on startup
no_browser: false

# Network bind addresses for the UI and OTLP receivers. Both families are
# served simultaneously (dual-stack). Leave a field blank to disable that family.
# IPv4: 127.0.0.1 = localhost; 0.0.0.0 = all interfaces (LAN / docker).
# IPv6: ::1 = localhost; :: = all interfaces.
bind_address_v4: 127.0.0.1
bind_address_v6: ::1

# OTLP forwarding: forward received spans/logs/metrics to upstream backends
# forward:
#   - http://tempo:4318
#   - http://otelcollector:4318
# Fraction of payloads to forward upstream (0.0–1.0). 1.0 forwards everything.
forward_sample: 1.0

# TLS: enable HTTPS/gRPC-TLS on the UI, API, and both OTLP receivers.
# Both keys must be set together; leave both blank for plaintext (default).
# tls_cert: /path/to/cert.pem
# tls_key:  /path/to/key.pem

# Bearer token: require this secret on all inbound OTLP and API requests.
# The browser UI is seeded automatically via /api/health?token=<value> on
# first visit. Set via env var SPANIEL_BEARER_TOKEN to avoid storing in config.
# bearer_token: ""
`

// globalConfigPath returns the path to ~/.spaniel/config.yaml.
func globalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".spaniel", "config.yaml")
}

// initViper sets up viper with the right search paths and defaults, then reads
// the config file (creating it on first run). Must be called before any flag
// values are accessed.
//
// Priority (high → low): CLI flags > project .spaniel.yaml > global config > defaults.
func initViper(v *viper.Viper) {
	// Defaults
	v.SetDefault("port", 8080)
	v.SetDefault("db_path", defaultDBPath())
	v.SetDefault("retention_days", 7)
	v.SetDefault("max_sessions", 50)
	v.SetDefault("max_db_size_mb", 500)
	v.SetDefault("otlp_grpc_port", 4317)
	v.SetDefault("otlp_http_port", 4318)
	v.SetDefault("no_browser", false)
	v.SetDefault("bind_address_v4", "127.0.0.1")
	v.SetDefault("bind_address_v6", "::1")
	v.SetDefault("forward_sample", 1.0)
	v.SetDefault("forward_spool_dir", "")
	v.SetDefault("forward_max_spool_mb", 100)
	v.SetDefault("forward_retry_max", "30s")
	v.SetDefault("tls_cert", "")
	v.SetDefault("tls_key", "")
	v.SetDefault("bearer_token", "")
	v.SetDefault("self_telemetry_endpoint", "")
	v.SetDefault("self_telemetry_service", "spaniel")
	v.SetDefault("self_telemetry_insecure", true)

	// ENV: SPANIEL_PORT, SPANIEL_DB_PATH, etc.
	v.SetEnvPrefix("SPANIEL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 1. Load global config (~/.spaniel/config.yaml).
	home, _ := os.UserHomeDir()
	v.SetConfigType("yaml")
	v.SetConfigFile(filepath.Join(home, ".spaniel", "config.yaml"))
	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			_ = bootstrapConfig()
			_ = v.ReadInConfig()
		}
	}

	// 2. Merge project-level .spaniel.yaml on top (if it exists).
	projectCfg := ".spaniel.yaml"
	if _, err := os.Stat(projectCfg); err == nil {
		v.SetConfigFile(projectCfg)
		_ = v.MergeInConfig()
	}
}

// bootstrapConfig writes the default config to ~/.spaniel/config.yaml.
func bootstrapConfig() error {
	p := globalConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(defaultConfig), 0o640)
}

// bindRootFlags makes viper aware of the cobra flag values so CLI flags win.
// Call after ParseFlags (i.e. in PersistentPreRunE or the command's RunE).
func bindRootFlags(v *viper.Viper, cmd *cobra.Command) {
	for _, name := range []string{"port", "db-path", "retention", "max-sessions", "max-db-size", "no-browser", "dev"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			f = cmd.PersistentFlags().Lookup(name)
		}
		if f == nil || !f.Changed {
			continue
		}
		// Map flag name to viper key
		key := strings.ReplaceAll(name, "-", "_")
		switch key {
		case "retention":
			key = "retention_days"
		case "max_db_size":
			key = "max_db_size_mb"
		}
		v.Set(key, f.Value.String())
	}
}

// configShow prints the effective config as YAML.
func configShow(v *viper.Viper) {
	all := v.AllSettings()
	b, _ := yaml.Marshal(all)
	fmt.Printf("# effective config (source: %s)\n", v.ConfigFileUsed())
	fmt.Print(string(b))
}

// configSetKey writes key=value into the global config file.
func configSetKey(key, value string) error {
	// Read current global config into a fresh viper to avoid project-level merge.
	v := viper.New()
	v.SetConfigFile(globalConfigPath())
	_ = v.ReadInConfig()
	v.Set(key, value)
	return v.WriteConfig()
}

// configSubcommand returns the `spaniel config` cobra.Command tree.
func configSubcommand(v *viper.Viper) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Show or edit spaniel configuration",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configShow(v)
			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a value in the global config file (~/.spaniel/config.yaml)",
		Example: `  spaniel config set port 9090
  spaniel config set retention_days 14
  spaniel config set no_browser true`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := configSetKey(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("set %s = %s  (in %s)\n", args[0], args[1], globalConfigPath())
			return nil
		},
	}

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path of the active config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			used := v.ConfigFileUsed()
			if used == "" {
				used = globalConfigPath() + " (not yet created)"
			}
			fmt.Println(used)
			return nil
		},
	}

	configCmd.AddCommand(showCmd, setCmd, pathCmd)
	return configCmd
}
