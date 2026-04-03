package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// OSVScanner runs Google's osv-scanner against lockfiles found in Verdaccio storage.
// Uses a pre-downloaded offline database for air-gapped operation.
type OSVScanner struct {
	databasePath string
}

func NewOSVScanner(databasePath string) *OSVScanner {
	return &OSVScanner{databasePath: databasePath}
}

func (s *OSVScanner) Name() string { return "osv" }

func (s *OSVScanner) Scan(ctx context.Context, storagePath string) ([]Finding, error) {
	// OSV-Scanner scans lockfiles, not tarballs. In the mirror-update workflow,
	// lockfiles are in the fixture repos that were just installed. For the storage
	// scan, we scan the packument metadata for known-vulnerable versions.
	//
	// Run osv-scanner in recursive mode against the storage directory.
	// It will find package.json files and match versions against the offline DB.
	args := []string{
		"scan",
		"--offline-vulnerabilities",
		"--format", "json",
		"--recursive",
		storagePath,
	}

	cmd := exec.CommandContext(ctx, "osv-scanner", args...)
	cmd.Env = append(cmd.Environ(), "OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY="+s.databasePath)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 = vulnerabilities found (normal).
			if exitErr.ExitCode() == 1 && len(out) > 0 {
				return s.parseOutput(out)
			}
			// Exit code 128 = no packages found (empty storage, OK).
			if exitErr.ExitCode() == 128 {
				return nil, nil
			}
			return nil, fmt.Errorf("osv-scanner exit %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("exec osv-scanner: %w", err)
	}

	// Exit 0 = no vulnerabilities.
	if len(out) == 0 {
		return nil, nil
	}
	return s.parseOutput(out)
}

// osvOutput matches osv-scanner's JSON output (simplified).
type osvOutput struct {
	Results []osvResult `json:"results"`
}

type osvResult struct {
	Source struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"source"`
	Packages []osvPackageResult `json:"packages"`
}

type osvPackageResult struct {
	Package struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Vulnerabilities []osvVuln `json:"vulnerabilities"`
}

type osvVuln struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

func (s *OSVScanner) parseOutput(data []byte) ([]Finding, error) {
	var parsed osvOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse osv-scanner output: %w", err)
	}

	var findings []Finding
	for _, result := range parsed.Results {
		for _, pkg := range result.Packages {
			for _, vuln := range pkg.Vulnerabilities {
				findings = append(findings, Finding{
					Scanner:  "osv",
					Package:  pkg.Package.Name,
					Version:  pkg.Package.Version,
					Rule:     vuln.ID,
					Severity: SeverityBlocking,
					Detail:   vuln.Summary,
				})
			}
		}
	}
	return findings, nil
}
