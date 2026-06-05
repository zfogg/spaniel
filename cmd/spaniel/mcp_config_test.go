package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// receiverConfigError: both ports disabled is an error; any one enabled is fine.
func TestReceiverConfigError(t *testing.T) {
	if err := receiverConfigError(0, 0); err == nil {
		t.Error("expected error when both OTLP ports are 0")
	}
	if err := receiverConfigError(4317, 0); err != nil {
		t.Errorf("gRPC-only should be allowed, got %v", err)
	}
	if err := receiverConfigError(0, 4318); err != nil {
		t.Errorf("HTTP-only should be allowed, got %v", err)
	}
	if err := receiverConfigError(4317, 4318); err != nil {
		t.Errorf("both enabled should be allowed, got %v", err)
	}
}

// run() must refuse to start (fast, before side effects) when no receiver is
// enabled, surfacing the receiverConfigError message.
func TestRun_RefusesWithoutReceiver(t *testing.T) {
	cfg := runConfig{
		DBPath:           "/dev/null/should-not-be-touched",
		NoBrowser:        true,
		BindAddressV4:    "127.0.0.1",
		OTLPGRPCPort:     0,
		OTLPHTTPPort:     0,
		SampleAlwaysKeep: "error",
	}
	err := run(cfg)
	if err == nil {
		t.Fatal("expected run() to return an error with no receiver enabled")
	}
	if !strings.Contains(err.Error(), "no OTLP receiver enabled") {
		t.Errorf("unexpected error: %v", err)
	}
}

// resolveConfig must treat an explicit OTLP port of 0 as "disabled" and NOT
// rewrite it to the default — this is what makes a receiver disableable.
func TestResolveConfig_OTLPPortZeroPreserved(t *testing.T) {
	v := viper.New()
	v.Set("otlp_grpc_port", 0)
	v.Set("otlp_http_port", 0)

	cfg := resolveConfig(v, &cobra.Command{}, 0, false, "", false, 0, 0, 0, nil, "", "", "")

	if cfg.OTLPGRPCPort != 0 {
		t.Errorf("otlp_grpc_port: got %d, want 0 (disabled, not rewritten to default)", cfg.OTLPGRPCPort)
	}
	if cfg.OTLPHTTPPort != 0 {
		t.Errorf("otlp_http_port: got %d, want 0 (disabled, not rewritten to default)", cfg.OTLPHTTPPort)
	}
}

// resolveConfig still applies the configured/default ports when not zeroed.
func TestResolveConfig_OTLPPortDefaults(t *testing.T) {
	v := viper.New()
	v.SetDefault("otlp_grpc_port", 4317)
	v.SetDefault("otlp_http_port", 4318)

	cfg := resolveConfig(v, &cobra.Command{}, 0, false, "", false, 0, 0, 0, nil, "", "", "")

	if cfg.OTLPGRPCPort != 4317 {
		t.Errorf("otlp_grpc_port: got %d, want 4317", cfg.OTLPGRPCPort)
	}
	if cfg.OTLPHTTPPort != 4318 {
		t.Errorf("otlp_http_port: got %d, want 4318", cfg.OTLPHTTPPort)
	}
}

// resolveConfig wires the MCP config keys through.
func TestResolveConfig_MCPKeys(t *testing.T) {
	v := viper.New()
	v.SetDefault("otlp_grpc_port", 4317)
	v.Set("mcp_enabled", true)
	v.Set("mcp_allow_writes", true)

	cfg := resolveConfig(v, &cobra.Command{}, 0, false, "", false, 0, 0, 0, nil, "", "", "")

	if !cfg.MCPEnabled {
		t.Error("MCPEnabled: got false, want true")
	}
	if !cfg.MCPAllowWrites {
		t.Error("MCPAllowWrites: got false, want true")
	}
}
