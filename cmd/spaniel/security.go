package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/zfogg/spaniel/internal/security"
)

func bindSecurityFlags(root *cobra.Command, v *viper.Viper) {
	root.Flags().Bool("api-token-security", false, "Require named API tokens on API, MCP, WebSockets and ingestion (env: SPANIEL_API_TOKEN_SECURITY)")
	root.Flags().String("encryption-password", "", "Password protecting security.duckdb (env: SPANIEL_ENCRYPTION_PASSWORD)")
	_ = v.BindPFlag("api_token_security", root.Flags().Lookup("api-token-security"))
	_ = v.BindPFlag("encryption_password", root.Flags().Lookup("encryption-password"))
}

func securitySource(v *viper.Viper, key string) string {
	if source := v.GetString(key + "_source"); source != "" {
		return source
	}
	return "config"
}

func openSecurity(cfg runConfig) (*security.Service, error) {
	v := cfg.Viper
	path := filepath.Join(filepath.Dir(cfg.DBPath), "security.duckdb")
	if cfg.DBPath == ":memory:" {
		path = ":memory:"
	}
	password := v.GetString("encryption_password")
	if password == "" {
		if path != ":memory:" {
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("encryption_password is missing for existing security.duckdb; restore it with --encryption-password or SPANIEL_ENCRYPTION_PASSWORD")
			}
		}
		password = security.RandomSecret()
		if path != ":memory:" && cfg.ConfigPath != "" {
			out := viper.New()
			out.SetConfigFile(cfg.ConfigPath)
			out.SetConfigPermissions(0o600)
			_ = out.ReadInConfig()
			out.Set("encryption_password", password)
			if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o700); err != nil {
				return nil, err
			}
			tmp := cfg.ConfigPath + ".tmp.yaml"
			if err := out.WriteConfigAs(tmp); err != nil {
				return nil, err
			}
			if err := os.Rename(tmp, cfg.ConfigPath); err != nil {
				return nil, err
			}
		}
		v.Set("encryption_password", password)
	}
	svc, err := security.Open(path, password)
	if err != nil {
		return nil, err
	}
	svc.SetPublicURL(cfg.PublicURL)
	if err = svc.ImportLegacy(cfg.BearerToken); err != nil {
		_ = svc.Close()
		return nil, err
	}
	enabled := v.GetBool("api_token_security")
	if cfg.BearerToken != "" && !v.IsSet("api_token_security") {
		enabled = true
	}
	v.Set("api_token_security", enabled)
	svc.SetEnabledOnStartup(enabled)
	if cfg.BearerToken != "" && cfg.ConfigPath != "" {
		out := viper.New()
		out.SetConfigFile(cfg.ConfigPath)
		out.SetConfigPermissions(0o600)
		_ = out.ReadInConfig()
		out.Set("bearer_token", "")
		if securitySource(v, "api_token_security") == "config" {
			out.Set("api_token_security", enabled)
		}
		tmp := cfg.ConfigPath + ".tmp.yaml"
		if err := out.WriteConfigAs(tmp); err != nil {
			_ = svc.Close()
			return nil, err
		}
		if err := os.Rename(tmp, cfg.ConfigPath); err != nil {
			_ = svc.Close()
			return nil, err
		}
		v.Set("bearer_token", "")
	}
	return svc, nil
}
