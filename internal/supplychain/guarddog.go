package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// GuardDogScanner runs DataDog's guarddog against each tarball in Verdaccio storage.
// Detects malware patterns via Semgrep rules, YARA signatures, and metadata heuristics.
type GuardDogScanner struct {
	excludeRules []string
}

func NewGuardDogScanner(excludeRules []string) *GuardDogScanner {
	return &GuardDogScanner{excludeRules: excludeRules}
}

func (s *GuardDogScanner) Name() string { return "guarddog" }

func (s *GuardDogScanner) Scan(ctx context.Context, storagePath string) ([]Finding, error) {
	tarballs, err := findTarballs(storagePath)
	if err != nil {
		return nil, fmt.Errorf("enumerate tarballs: %w", err)
	}

	var (
		mu       sync.Mutex
		findings []Finding
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	for _, tarball := range tarballs {
		g.Go(func() error {
			results, err := s.scanTarball(ctx, tarball)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				findings = append(findings, Finding{
					Scanner:  s.Name(),
					Package:  tarballPackageName(tarball),
					Rule:     "scan-error",
					Severity: SeverityBlocking,
					Detail:   err.Error(),
				})
				return nil
			}
			findings = append(findings, results...)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return findings, err
	}
	return findings, nil
}

// guardDogOutput matches guarddog v2.9.0 JSON output format.
type guardDogOutput struct {
	Issues  int               `json:"issues"`
	Errors  map[string]string `json:"errors"`
	Results map[string]any    `json:"results"`
}

func (s *GuardDogScanner) scanTarball(ctx context.Context, tarball string) ([]Finding, error) {
	args := []string{"npm", "scan", tarball, "--output-format", "json"}
	for _, rule := range s.excludeRules {
		args = append(args, "--exclude-rules", rule)
	}

	cmd := exec.CommandContext(ctx, "guarddog", args...)
	out, err := cmd.Output()
	if err != nil {
		// guarddog exits non-zero when findings exist; only fail on exec error.
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, fmt.Errorf("exec guarddog: %w", err)
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	var parsed guardDogOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse guarddog output: %w", err)
	}

	pkg := tarballPackageName(tarball)
	var findings []Finding

	// Report guarddog-level errors (e.g. semgrep not found, npm 404) as warnings.
	for errName, errMsg := range parsed.Errors {
		findings = append(findings, Finding{
			Scanner:  "guarddog",
			Package:  pkg,
			Rule:     "error:" + errName,
			Severity: SeverityWarning,
			Detail:   errMsg,
		})
	}

	// In v2.9.0, each rule value is an empty object {} when clean.
	// Non-empty values contain actual findings.
	for rule, detail := range parsed.Results {
		detailMap, ok := detail.(map[string]any)
		if !ok || len(detailMap) == 0 {
			continue
		}
		findings = append(findings, Finding{
			Scanner:  "guarddog",
			Package:  pkg,
			Rule:     rule,
			Severity: SeverityBlocking,
			Detail:   fmt.Sprintf("%v", detail),
		})
	}
	return findings, nil
}

// tarballPackageName extracts the package name from a Verdaccio tarball path.
// Verdaccio stores tarballs at <storage>/<package>/<package>-<version>.tgz.
func tarballPackageName(tarball string) string {
	parts := strings.Split(tarball, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], "@") && i+1 < len(parts) {
			return parts[i] + "/" + parts[i+1]
		}
		if !strings.HasSuffix(parts[i], ".tgz") {
			return parts[i]
		}
	}
	return tarball
}
