package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type config struct {
	Timeout time.Duration
	Paths   []string
	Command []string
}

func main() {
	cfg, err := parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	if err := waitFiles(ctx, cfg.Paths); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	path, err := exec.LookPath(cfg.Command[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := syscall.Exec(path, cfg.Command, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parse(args []string) (config, error) {
	fs := flag.NewFlagSet("wait-files-exec", flag.ContinueOnError)
	cfg := config{}
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "maximum time to wait for all files")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	rest := fs.Args()
	separator := -1
	for i, arg := range rest {
		if arg == "--" {
			separator = i
			break
		}
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("--timeout must be positive")
	}
	if separator <= 0 {
		return config{}, errors.New("at least one file path and a -- command separator are required")
	}
	if separator == len(rest)-1 {
		return config{}, errors.New("command after -- is required")
	}
	cfg.Paths = append([]string(nil), rest[:separator]...)
	cfg.Command = append([]string(nil), rest[separator+1:]...)
	for _, path := range cfg.Paths {
		if path == "" {
			return config{}, errors.New("file path must not be empty")
		}
	}
	return cfg, nil
}

func waitFiles(ctx context.Context, paths []string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		missing := missingFiles(paths)
		if len(missing) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for files %v: %w", missing, ctx.Err())
		case <-ticker.C:
		}
	}
}

func missingFiles(paths []string) []string {
	missing := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			missing = append(missing, path)
		}
	}
	return missing
}
