package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeDefaultRemainsDev(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "-qp")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		if status, ok := err.(*exec.ExitError); !ok || status.ExitCode() != 1 {
			t.Fatalf("inspect make default: %v", err)
		}
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, ".DEFAULT_GOAL :=") {
			if strings.TrimSpace(strings.TrimPrefix(line, ".DEFAULT_GOAL :=")) != "dev" {
				t.Fatalf("changed default: %s", line)
			}
			return
		}
	}
	t.Fatal("make did not report a default target")
}
