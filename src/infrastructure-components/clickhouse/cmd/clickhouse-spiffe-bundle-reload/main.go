package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const restartTimeout = 45 * time.Second

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "clickhouse-spiffe-bundle-reload: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("clickhouse-spiffe-bundle-reload", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	bundlePath := fs.String("bundle", "", "SPIFFE bundle PEM path consumed by ClickHouse")
	statePath := fs.String("state-path", "", "path storing the last successfully loaded bundle SHA-256")
	unit := fs.String("unit", "clickhouse-server.service", "systemd unit to restart when the bundle changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" {
		return errors.New("--bundle is required")
	}
	if *statePath == "" {
		return errors.New("--state-path is required")
	}
	if err := validateUnit(*unit); err != nil {
		return err
	}

	bundle, err := os.ReadFile(*bundlePath)
	if err != nil {
		return fmt.Errorf("read bundle %s: %w", *bundlePath, err)
	}
	sum := sha256.Sum256(bundle)
	hash := hex.EncodeToString(sum[:])

	loaded, err := os.ReadFile(*statePath)
	switch {
	case err == nil && strings.TrimSpace(string(loaded)) == hash:
		fmt.Printf("bundle unchanged sha256=%s\n", hash)
		return nil
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
	default:
		return fmt.Errorf("read state %s: %w", *statePath, err)
	}

	if err := restartUnit(ctx, *unit); err != nil {
		return err
	}
	if err := writeState(*statePath, hash); err != nil {
		return err
	}
	fmt.Printf("restarted %s for bundle sha256=%s\n", *unit, hash)
	return nil
}

func validateUnit(unit string) error {
	if unit == "" {
		return errors.New("--unit is required")
	}
	if strings.ContainsAny(unit, " \t\r\n/'\"`\\") {
		return fmt.Errorf("unit %q contains unsupported characters", unit)
	}
	if !strings.HasSuffix(unit, ".service") {
		return fmt.Errorf("unit %q must be a .service unit", unit)
	}
	return nil
}

func restartUnit(ctx context.Context, unit string) error {
	ctx, cancel := context.WithTimeout(ctx, restartTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "restart", unit).CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("systemctl restart %s timed out: %w", unit, ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeState(path, hash string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".bundle-sha256-*")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.WriteString(hash + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write state temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o640); err != nil {
		return fmt.Errorf("chmod state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace state %s: %w", path, err)
	}
	return nil
}
