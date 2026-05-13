package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	var root string
	var path string
	var out string
	flag.StringVar(&root, "root", "", "Smithy build output tree")
	flag.StringVar(&path, "path", "", "artifact path relative to root")
	flag.StringVar(&out, "out", "", "output file")
	flag.Parse()

	if err := copyArtifact(root, path, out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func copyArtifact(root, path, out string) error {
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
	_, copyErr := io.Copy(dst, in)
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("copy Smithy artifact %s: %w", clean, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close output %s: %w", out, closeErr)
	}
	return nil
}
