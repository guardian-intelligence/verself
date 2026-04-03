package supplychain

import (
	"context"
	"fmt"
	"time"
)

// AgeScanner rejects package versions published less than minDays ago.
// Reads the packument time field from Verdaccio's cached metadata.
// Verdaccio preserves upstream time data via mergeUplinkTimeIntoLocalNext().
type AgeScanner struct {
	minDays int
}

func NewAgeScanner(minDays int) *AgeScanner {
	return &AgeScanner{minDays: minDays}
}

func (s *AgeScanner) Name() string { return "age" }

func (s *AgeScanner) Scan(_ context.Context, storagePath string) ([]Finding, error) {
	packageDirs, err := findPackageDirs(storagePath)
	if err != nil {
		return nil, fmt.Errorf("enumerate packages: %w", err)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(s.minDays) * 24 * time.Hour)
	var findings []Finding

	for _, dir := range packageDirs {
		p, err := loadPackument(dir)
		if err != nil {
			// Missing or corrupt packument — flag but don't block.
			findings = append(findings, Finding{
				Scanner:  s.Name(),
				Package:  dir,
				Rule:     "packument-read-error",
				Severity: SeverityWarning,
				Detail:   err.Error(),
			})
			continue
		}

		if p.Time == nil {
			continue
		}

		for version, timestamp := range p.Time {
			if version == "created" || version == "modified" {
				continue
			}
			publishedAt, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
				// Try alternate format npm sometimes uses.
				publishedAt, err = time.Parse("2006-01-02T15:04:05.000Z", timestamp)
				if err != nil {
					continue
				}
			}
			if publishedAt.After(cutoff) {
				findings = append(findings, Finding{
					Scanner:  s.Name(),
					Package:  p.Name,
					Version:  version,
					Rule:     "min-release-age",
					Severity: SeverityBlocking,
					Detail:   fmt.Sprintf("version %s published %s, minimum age %d days", version, publishedAt.Format(time.RFC3339), s.minDays),
				})
			}
		}
	}

	return findings, nil
}
