package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	var jar string
	var entry string
	var out string
	flag.StringVar(&jar, "jar", "", "JAR file to read")
	flag.StringVar(&entry, "entry", "", "Smithy model path inside the JAR")
	flag.StringVar(&out, "out", "", "output file")
	flag.Parse()

	if err := extractModel(jar, entry, out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func extractModel(jarPath, entryPath, outPath string) error {
	jarPath = strings.TrimSpace(jarPath)
	entryPath = strings.TrimSpace(entryPath)
	outPath = strings.TrimSpace(outPath)
	if jarPath == "" || entryPath == "" || outPath == "" {
		return errors.New("-jar, -entry, and -out are required")
	}
	if filepath.IsAbs(entryPath) {
		return fmt.Errorf("JAR entry must be relative: %s", entryPath)
	}
	cleanEntry := path.Clean(entryPath)
	if cleanEntry == "." || cleanEntry == ".." || strings.HasPrefix(cleanEntry, "../") {
		return fmt.Errorf("JAR entry escapes archive root: %s", entryPath)
	}

	archive, err := zip.OpenReader(jarPath)
	if err != nil {
		return fmt.Errorf("open JAR %s: %w", jarPath, err)
	}
	defer archive.Close()

	for _, file := range archive.File {
		if file.Name != cleanEntry {
			continue
		}
		if file.FileInfo().IsDir() {
			return fmt.Errorf("JAR entry is a directory: %s", cleanEntry)
		}
		return writeEntry(file, outPath)
	}
	return fmt.Errorf("JAR entry not found: %s", cleanEntry)
}

func writeEntry(file *zip.File, outPath string) error {
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("open JAR entry %s: %w", file.Name, err)
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	dst, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output %s: %w", outPath, err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("copy JAR entry %s: %w", file.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close output %s: %w", outPath, closeErr)
	}
	return nil
}
