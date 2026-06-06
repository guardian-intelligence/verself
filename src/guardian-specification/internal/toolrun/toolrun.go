package toolrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/verself/guardian-specification/internal/toolcatalog"
)

type Options struct {
	CacheRoot string
	WorkDir   string
	Stdout    io.Writer
	Stderr    io.Writer
}

func EnsureExecutable(ctx context.Context, tool toolcatalog.ResolvedTool, opts Options) (string, error) {
	if tool.Admission != "admitted" {
		return "", fmt.Errorf("guardian: tool %q digest %s is not admitted", tool.Name, tool.Digest)
	}
	cacheRoot, err := cacheRoot(opts.CacheRoot)
	if err != nil {
		return "", err
	}
	path := executablePath(cacheRoot, tool)
	if ok, err := verifyFileDigest(path, tool.Digest); err != nil {
		return "", err
	} else if ok {
		return path, nil
	}
	if len(tool.Mirrors) == 0 {
		return "", fmt.Errorf("guardian: tool %q digest %s is absent from cache and has no mirrors", tool.Name, tool.Digest)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create tool cache: %w", err)
	}
	tmp := path + ".tmp." + fmt.Sprint(os.Getpid())
	defer func() { _ = os.Remove(tmp) }()
	var last error
	for _, mirror := range tool.Mirrors {
		if err := fetchMirror(ctx, mirror, tmp); err != nil {
			last = err
			continue
		}
		if ok, err := verifyFileDigest(tmp, tool.Digest); err != nil {
			last = err
			continue
		} else if !ok {
			last = fmt.Errorf("mirror %s did not match %s", mirror, tool.Digest)
			continue
		}
		if err := os.Chmod(tmp, 0o755); err != nil {
			return "", fmt.Errorf("chmod cached tool: %w", err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", fmt.Errorf("promote cached tool: %w", err)
		}
		return path, nil
	}
	return "", fmt.Errorf("guardian: fetch tool %q: %w", tool.Name, last)
}

func Exec(ctx context.Context, tool toolcatalog.ResolvedTool, args []string, opts Options) int {
	executable, err := EnsureExecutable(ctx, tool, opts)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "%v\n", err)
		return 1
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	err = cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	_, _ = fmt.Fprintf(opts.Stderr, "guardian: exec tool %q: %v\n", tool.Name, err)
	return 1
}

func cacheRoot(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	if value := os.Getenv("XDG_CACHE_HOME"); strings.TrimSpace(value) != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("XDG_CACHE_HOME must be absolute: %s", value)
		}
		return filepath.Join(value, "guardian", "tools"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "guardian", "tools"), nil
}

func executablePath(cacheRoot string, tool toolcatalog.ResolvedTool) string {
	hexDigest := strings.TrimPrefix(tool.Digest, "sha256:")
	return filepath.Join(cacheRoot, "sha256", hexDigest, "bin", tool.Executable)
}

func verifyFileDigest(path string, digest string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open cached tool: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("hash cached tool: %w", err)
	}
	observed := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	return observed == digest, nil
}

func fetchMirror(ctx context.Context, mirror string, dst string) error {
	parsed, err := url.Parse(mirror)
	if err != nil {
		return fmt.Errorf("parse mirror %q: %w", mirror, err)
	}
	switch parsed.Scheme {
	case "file":
		return copyFile(parsed.Path, dst)
	case "https":
		return fetchHTTPS(ctx, mirror, dst)
	default:
		return fmt.Errorf("unsupported mirror scheme %q", parsed.Scheme)
	}
}

func fetchHTTPS(ctx context.Context, mirror string, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror, nil)
	if err != nil {
		return fmt.Errorf("create mirror request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download mirror: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download mirror returned HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create cached tool temp: %w", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write cached tool temp: %w", err)
	}
	return nil
}

func copyFile(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open mirror file: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create cached tool temp: %w", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("write cached tool temp: %w", err)
	}
	return nil
}
