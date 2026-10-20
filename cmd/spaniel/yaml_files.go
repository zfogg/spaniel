package main

import (
	"fmt"
	"os"
)

func writeGeneratedYAMLID(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("rewrite YAML: %w", err)
	}
	return nil
}
