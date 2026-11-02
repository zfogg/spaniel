package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestSecurityFlagEnvironmentAndConfigPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, env, flag string
		want            bool
		source          string
	}{{"config", "", "", true, "config"}, {"environment", "false", "", false, "environment"}, {"flag", "true", "--api-token-security=false", false, "flag"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SPANIEL_API_TOKEN_SECURITY", tc.env)
			v := viper.New()
			v.SetEnvPrefix("SPANIEL")
			v.AutomaticEnv()
			_ = v.MergeConfigMap(map[string]any{"api_token_security": true})
			cmd := &cobra.Command{}
			bindSecurityFlags(cmd, v)
			if tc.flag != "" {
				if err := cmd.ParseFlags([]string{tc.flag}); err != nil {
					t.Fatal(err)
				}
			}
			cfg := resolveConfig(v, cmd, 0, false, "", false, 0, 0, 0, nil, "", "", "")
			if cfg.Viper.GetBool("api_token_security") != tc.want || securitySource(v, "api_token_security") != tc.source {
				t.Fatal("incorrect precedence or override source")
			}
		})
	}
}

func TestSecurityStartupMigratesLegacyWithoutLosingProtection(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("bearer_token: example-legacy-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{DBPath: filepath.Join(dir, "telemetry.duckdb"), ConfigPath: configPath, Viper: v, BearerToken: "example-legacy-secret"}
	svc, err := openSecurity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.Enabled() {
		t.Fatal("legacy installation silently lost authentication")
	}
	if _, code := svc.Validate(cfg.BearerToken); code != "" {
		t.Fatal(code)
	}
	_ = svc.Close()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cfg.BearerToken) {
		t.Fatal("legacy plaintext token remained in config")
	}
	loaded := viper.New()
	loaded.SetConfigFile(configPath)
	_ = loaded.ReadInConfig()
	if loaded.GetString("encryption_password") == "" || !loaded.GetBool("api_token_security") {
		t.Fatal("migration was not persisted")
	}
	// Operator recovery through a higher-priority explicit false must retain
	// the token records and allow a replacement to be created.
	loaded.Set("api_token_security", false)
	cfg.Viper = loaded
	cfg.BearerToken = ""
	svc, err = openSecurity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if svc.Enabled() {
		t.Fatal("recovery override ignored")
	}
	if len(svc.List()) != 1 {
		t.Fatal("recovery deleted tokens")
	}
}
