package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// JSXRayScanner runs NodeSecure js-x-ray AST analysis against each tarball
// in Verdaccio storage. Detects obfuscation, eval chains, data exfiltration,
// and other code-level supply chain attack patterns.
type JSXRayScanner struct{}

func NewJSXRayScanner() *JSXRayScanner { return &JSXRayScanner{} }

func (s *JSXRayScanner) Name() string { return "jsxray" }

func (s *JSXRayScanner) Scan(ctx context.Context, storagePath string) ([]Finding, error) {
	workerPath, err := resolveWorkerPath()
	if err != nil {
		return nil, fmt.Errorf("locate jsxray worker: %w", err)
	}

	tarballs, err := findTarballs(storagePath)
	if err != nil {
		return nil, fmt.Errorf("enumerate tarballs: %w", err)
	}

	var findings []Finding
	for _, tarball := range tarballs {
		results, err := s.scanTarball(ctx, workerPath, tarball)
		if err != nil {
			findings = append(findings, Finding{
				Scanner:  s.Name(),
				Package:  tarballPackageName(tarball),
				Rule:     "scan-error",
				Severity: SeverityWarning, // js-x-ray errors are warnings, not blocking.
				Detail:   err.Error(),
			})
			continue
		}
		findings = append(findings, results...)
	}
	return findings, nil
}

type jsxrayOutput struct {
	Warnings []jsxrayWarning `json:"warnings"`
}

type jsxrayWarning struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	File     string `json:"file"`
	Detail   string `json:"detail"`
}

func (s *JSXRayScanner) scanTarball(ctx context.Context, workerPath, tarball string) ([]Finding, error) {
	// Extract tarball to temp dir for AST analysis.
	tmpDir, err := os.MkdirTemp("", "jsxray-scan-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	extractCmd := exec.CommandContext(ctx, "tar", "-xzf", tarball, "-C", tmpDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("extract tarball: %s: %w", strings.TrimSpace(string(out)), err)
	}

	cmd := exec.CommandContext(ctx, "node", workerPath, tmpDir)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("jsxray worker exit %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("exec jsxray worker: %w", err)
	}

	var parsed jsxrayOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse jsxray output: %w", err)
	}

	pkg := tarballPackageName(tarball)
	var findings []Finding
	for _, w := range parsed.Warnings {
		severity := SeverityInfo
		switch w.Severity {
		case "warning", "critical":
			severity = SeverityBlocking
		}
		// Skip encoded-literal info noise.
		if w.Kind == "encoded-literal" {
			continue
		}
		findings = append(findings, Finding{
			Scanner:  s.Name(),
			Package:  pkg,
			Rule:     w.Kind,
			Severity: severity,
			Detail:   w.Detail,
		})
	}
	return findings, nil
}

// resolveWorkerPath finds the jsxray-worker.mjs file shipped alongside this package.
func resolveWorkerPath() (string, error) {
	candidates := []string{
		// Server deployment: Ansible deploys the worker here with a node_modules
		// symlink so ESM resolution finds @nodesecure/js-x-ray.
		"/opt/forge-metal/scan/jsxray-worker.mjs",
	}

	// Check relative to the running binary (non-Nix deployments).
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "jsxray-worker.mjs"))
	}

	// Source tree location (development).
	_, thisFile, _, _ := runtime.Caller(0)
	candidates = append(candidates, filepath.Join(filepath.Dir(thisFile), "jsxray-worker.mjs"))

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("jsxray-worker.mjs not found; checked: %v", candidates)
}
