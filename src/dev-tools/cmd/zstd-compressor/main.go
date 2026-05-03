package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	zstdPath := os.Getenv("ZSTD_PATH")
	if zstdPath == "" {
		zstdPath = "/usr/bin/zstd"
	}
	cmd := exec.Command(zstdPath, "-T0", "-6", "-")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "zstd-compressor: %v\n", err)
		os.Exit(1)
	}
}
