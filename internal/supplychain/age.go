package supplychain

import (
	"context"
	"fmt"
	"os"
	"strings"
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

		// Only check versions that have a cached tarball. The packument
		// contains timestamps for ALL versions ever published, but only
		// installed versions have tarballs in storage.
		cached := cachedVersions(dir, p.Name)

		for version, timestamp := range p.Time {
			if version == "created" || version == "modified" {
				continue
			}
			if !cached[version] {
				continue
			}
			publishedAt, err := time.Parse(time.RFC3339, timestamp)
			if err != nil {
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

// cachedVersions returns the set of versions that have a .tgz tarball in the
// package directory. Verdaccio stores tarballs as <pkg>-<version>.tgz.
func cachedVersions(packageDir, packageName string) map[string]bool {
	versions := make(map[string]bool)
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return versions
	}
	// For scoped packages (@scope/name), strip the scope prefix from the tarball name.
	baseName := packageName
	if i := strings.LastIndex(baseName, "/"); i >= 0 {
		baseName = baseName[i+1:]
	}
	prefix := baseName + "-"
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".tgz") || !strings.HasPrefix(name, prefix) {
			continue
		}
		version := strings.TrimPrefix(name, prefix)
		version = strings.TrimSuffix(version, ".tgz")
		versions[version] = true
	}
	return versions
}
