package supplychain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GateConfig controls scanner behavior and allowlisting.
type GateConfig struct {
	MinReleaseAgeDays int               `toml:"min_release_age_days"`
	GuardDogExclude   []string          `toml:"guarddog_exclude_rules"`
	OSVDatabasePath   string            `toml:"osv_database_path"`
	Allowlist         map[string]string `toml:"allowlist"` // "pkg:rule" → reason
}

// GateResult is the aggregate outcome of all scanners.
type GateResult struct {
	Pass            bool                   `json:"pass"`
	Results         map[string]ScanResult  `json:"results"`
	TarballsScanned int                    `json:"tarballs_scanned"`
	Duration        time.Duration          `json:"duration_ns"`
}

// Summary returns a one-line description of the gate outcome.
func (r GateResult) Summary() string {
	var parts []string
	for name, sr := range r.Results {
		if bc := sr.BlockingCount(); bc > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", name, bc))
		}
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, " ")
}

// FindingsByScanner returns finding counts per scanner for telemetry.
func (r GateResult) FindingsByScanner() map[string]int {
	m := make(map[string]int, len(r.Results))
	for name, sr := range r.Results {
		m[name] = len(sr.Findings)
	}
	return m
}

// Gate orchestrates a set of scanners against a Verdaccio storage directory.
// Scanner errors are treated as blocking (fail-closed).
type Gate struct {
	Scanners []Scanner
	Config   GateConfig
}

// NewGate creates a Gate with the standard scanner set.
func NewGate(cfg GateConfig) *Gate {
	if cfg.MinReleaseAgeDays <= 0 {
		cfg.MinReleaseAgeDays = 3
	}
	if cfg.OSVDatabasePath == "" {
		cfg.OSVDatabasePath = "/var/lib/osv-scanner"
	}

	scanners := []Scanner{
		NewAgeScanner(cfg.MinReleaseAgeDays),
		NewGuardDogScanner(cfg.GuardDogExclude),
		NewJSXRayScanner(),
		NewOSVScanner(cfg.OSVDatabasePath),
	}

	return &Gate{
		Scanners: scanners,
		Config:   cfg,
	}
}

// Run executes all scanners sequentially and returns the aggregate result.
// Any scanner error is converted to a blocking finding (fail-closed).
func (g *Gate) Run(ctx context.Context, storagePath string) (GateResult, error) {
	start := time.Now()
	result := GateResult{
		Pass:    true,
		Results: make(map[string]ScanResult, len(g.Scanners)),
	}

	tarballs, err := findTarballs(storagePath)
	if err != nil {
		return result, fmt.Errorf("enumerate tarballs: %w", err)
	}
	result.TarballsScanned = len(tarballs)

	for _, scanner := range g.Scanners {
		scanStart := time.Now()
		findings, err := scanner.Scan(ctx, storagePath)
		scanDuration := time.Since(scanStart)

		if err != nil {
			// Fail-closed: scanner error becomes a blocking finding.
			findings = append(findings, Finding{
				Scanner:  scanner.Name(),
				Package:  "",
				Rule:     "scanner-error",
				Severity: SeverityBlocking,
				Detail:   err.Error(),
			})
		}

		// Apply allowlist.
		findings = g.applyAllowlist(findings)

		sr := ScanResult{
			Scanner:  scanner.Name(),
			Findings: findings,
			Duration: scanDuration,
		}
		result.Results[scanner.Name()] = sr

		if sr.BlockingCount() > 0 {
			result.Pass = false
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// applyAllowlist downgrades matching findings from Blocking to Warning.
func (g *Gate) applyAllowlist(findings []Finding) []Finding {
	if len(g.Config.Allowlist) == 0 {
		return findings
	}
	for i := range findings {
		key := findings[i].Package + ":" + findings[i].Rule
		if _, ok := g.Config.Allowlist[key]; ok && findings[i].Severity == SeverityBlocking {
			findings[i].Severity = SeverityWarning
		}
	}
	return findings
}
