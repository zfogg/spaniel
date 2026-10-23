package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"testing"
)

func TestDefinitionDirectoryFlagsApplyToReloads(t *testing.T) {
	v := viper.New()
	cmd := &cobra.Command{}
	cmd.Flags().String("dashboards-dir", "", "")
	cmd.Flags().String("alerts-dir", "", "")
	if err := cmd.Flags().Parse([]string{"--dashboards-dir", "/project/dashboards", "--alerts-dir", "/project/alerts"}); err != nil {
		t.Fatal(err)
	}
	bindRootFlags(v, cmd)
	if v.GetString("dashboards_dir") != "/project/dashboards" || v.GetString("alerts_dir") != "/project/alerts" {
		t.Fatal("reload callbacks did not receive CLI directory overrides")
	}
}

func TestDefinitionDirectoryDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SPANIEL_ALERTS_DIR", "")
	t.Setenv("SPANIEL_DASHBOARDS_DIR", "")
	t.Chdir(t.TempDir())
	v := viper.New()
	initViper(v)
	if v.GetString("dashboards_dir") != "~/.spaniel/dashboards" || v.GetString("alerts_dir") != "~/.spaniel/alerts" {
		t.Fatal("both default definition directories must be watched")
	}
}
