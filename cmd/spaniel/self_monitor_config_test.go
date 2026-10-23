package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestSelfMonitoringRequiresOptIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SPANIEL_SELF_MONITOR", "")
	t.Chdir(home)
	v := viper.New()
	initViper(v)
	resolve := func() runConfig {
		return resolveConfig(v, &cobra.Command{}, 0, false, "", false, 0, 0, 0, nil, "", "", "")
	}
	if cfg := resolve(); cfg.SelfMonitor || cfg.SelfTelemetryEndpoint != "" {
		t.Fatalf("fresh configuration enabled self monitoring: monitor=%v endpoint=%q", cfg.SelfMonitor, cfg.SelfTelemetryEndpoint)
	}
	v.Set("self_monitor", true)
	if cfg := resolve(); !cfg.SelfMonitor || cfg.SelfTelemetryEndpoint != "localhost:4317" {
		t.Fatalf("explicit opt-in did not enable self monitoring: monitor=%v endpoint=%q", cfg.SelfMonitor, cfg.SelfTelemetryEndpoint)
	}
	v.Set("self_monitor", false)
	if cfg := resolve(); cfg.SelfMonitor || cfg.SelfTelemetryEndpoint != "" {
		t.Fatalf("explicit opt-out retained self monitoring: monitor=%v endpoint=%q", cfg.SelfMonitor, cfg.SelfTelemetryEndpoint)
	}
}
