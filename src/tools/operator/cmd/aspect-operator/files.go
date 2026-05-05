package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func readYAMLFile(path string, target any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
