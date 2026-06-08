package toolrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/verself/guardian-specification/internal/toolcatalog"
)

// ToolsRootEnv names the environment variable the golden Guardian image injects
// to point at its self-contained tool closure (/opt/guardian/tools). When unset,
// the resolver uses the workspace-local root that `aspect dev install`
// materializes. These are two explicit, declared locations — not a network-fetch
// fallback. A requested tool that is absent under the resolved root fails loud.
const ToolsRootEnv = "GUARDIAN_TOOLS_ROOT"

// workspaceToolsRootRel is the canonical in-repo tool root for development. The
// stage-zero `aspect dev install` path lays the catalog's tool closure here so a
// single resolution code path serves both the deployed image and a dev checkout.
const workspaceToolsRootRel = ".verself/tools/root"

type Options struct {
	WorkspaceRoot string
	WorkDir       string
	Stdout        io.Writer
	Stderr        io.Writer
}

// Resolution is the located, executable tool inside the resolved tool root.
type Resolution struct {
	Root       string
	Executable string
	// Source is "image" when GUARDIAN_TOOLS_ROOT is set, "workspace" otherwise.
	// Integrity of the bytes is anchored to the cosign-verified image manifest
	// digest (verified once at materialization), not to a per-invocation re-hash
	// of every binary — re-hashing bazel-sized executables on every `guardian
	// run` would defeat the latency win the image exists to provide.
	Source string
}

// Locate resolves a catalog tool to its on-disk executable inside the tool root.
func Locate(tool toolcatalog.ResolvedTool, opts Options) (Resolution, error) {
	root, source, err := toolsRoot(opts.WorkspaceRoot)
	if err != nil {
		return Resolution{}, err
	}
	path := filepath.Join(root, tool.Name, "bin", tool.Executable)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Resolution{}, fmt.Errorf("guardian: tool %q is absent from the tool root %s (expected %s); run `aspect dev install --install-shims` to materialize the dev tool root, or set %s to the golden image tool root", tool.Name, root, path, ToolsRootEnv)
		}
		return Resolution{}, fmt.Errorf("stat tool %q: %w", tool.Name, err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return Resolution{}, fmt.Errorf("guardian: tool %q at %s is not an executable file", tool.Name, path)
	}
	return Resolution{Root: root, Executable: path, Source: source}, nil
}

func Exec(ctx context.Context, tool toolcatalog.ResolvedTool, args []string, opts Options) int {
	resolution, err := Locate(tool, opts)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "%v\n", err)
		return 1
	}
	cmd := exec.CommandContext(ctx, resolution.Executable, args...)
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

func toolsRoot(workspaceRoot string) (string, string, error) {
	if value := strings.TrimSpace(os.Getenv(ToolsRootEnv)); value != "" {
		if !filepath.IsAbs(value) {
			return "", "", fmt.Errorf("%s must be absolute: %s", ToolsRootEnv, value)
		}
		return value, "image", nil
	}
	if strings.TrimSpace(workspaceRoot) == "" {
		return "", "", fmt.Errorf("guardian: tool root is undetermined: set %s or run inside a Guardian workspace", ToolsRootEnv)
	}
	return filepath.Join(workspaceRoot, filepath.FromSlash(workspaceToolsRootRel)), "workspace", nil
}
