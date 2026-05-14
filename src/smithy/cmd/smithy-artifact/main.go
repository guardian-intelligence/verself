package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	var root string
	var path string
	var out string
	var yamlOut bool
	flag.StringVar(&root, "root", "", "Smithy build output tree")
	flag.StringVar(&path, "path", "", "artifact path relative to root")
	flag.StringVar(&out, "out", "", "output file")
	flag.BoolVar(&yamlOut, "yaml", false, "convert the artifact bytes to YAML")
	flag.Parse()

	if err := copyArtifact(root, path, out, yamlOut); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func copyArtifact(root, path, out string, yamlOut bool) error {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	out = strings.TrimSpace(out)
	if root == "" || path == "" || out == "" {
		return errors.New("-root, -path, and -out are required")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("artifact path must be relative: %s", path)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact path escapes root: %s", path)
	}
	src := filepath.Join(root, clean)
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open Smithy artifact %s: %w", clean, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	dst, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create output %s: %w", out, err)
	}
	var copyErr error
	if yamlOut {
		copyErr = writeYAML(dst, in)
	} else {
		_, copyErr = io.Copy(dst, in)
	}
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("copy Smithy artifact %s: %w", clean, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close output %s: %w", out, closeErr)
	}
	return nil
}

func writeYAML(dst io.Writer, src io.Reader) error {
	var node yaml.Node
	if err := yaml.NewDecoder(src).Decode(&node); err != nil {
		return fmt.Errorf("decode artifact as YAML/JSON: %w", err)
	}
	clearYAMLStyle(&node)
	encoder := yaml.NewEncoder(dst)
	encoder.SetIndent(2)
	if err := encoder.Encode(&node); err != nil {
		_ = encoder.Close()
		return fmt.Errorf("encode artifact as YAML: %w", err)
	}
	return encoder.Close()
}

func clearYAMLStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	node.Style = 0
	for _, child := range node.Content {
		clearYAMLStyle(child)
	}
}
