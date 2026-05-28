package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const releaseTagPrefix = "mksk-v"

var (
	workspacePackageRE = regexp.MustCompile(`^\s*\[workspace\.package\]\s*(?:#.*)?$`)
	sectionHeaderRE    = regexp.MustCompile(`^\s*\[.*\]\s*(?:#.*)?$`)
	tomlVersionRE      = regexp.MustCompile(`^\s*version\s*=\s*"([^"]+)"\s*(?:#.*)?$`)
	rcTagRE            = regexp.MustCompile(`^` + regexp.QuoteMeta(releaseTagPrefix) + `((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))-rc\.([0-9]+)$`)
)

type releaseIntent interface {
	deriveSubject(context.Context, releaseDerivationConfig) (ReleaseSubject, error)
}

type releaseDerivationConfig struct {
	RepoRoot   string
	OutputRoot string
	Platform   Platform
	Flavor     string
	Now        time.Time
}

type nightlyIntent struct {
	SourceRef string
}

type rcIntent struct {
	SourceRef string
}

type stableIntent struct {
	SourceRef string
	FromRC    string
}

type exactIntent struct {
	Channel      Channel
	Version      string
	SourceRef    string
	SourceCommit string
}

func (i nightlyIntent) deriveSubject(ctx context.Context, cfg releaseDerivationConfig) (ReleaseSubject, error) {
	sourceRef := defaultString(i.SourceRef, defaultSourceRef)
	commit, err := resolveCommit(ctx, cfg.RepoRoot, sourceRef)
	if err != nil {
		return ReleaseSubject{}, err
	}
	base, err := mkskBaseVersionAtCommit(ctx, cfg.RepoRoot, commit)
	if err != nil {
		return ReleaseSubject{}, err
	}
	date := cfg.Now.UTC().Format("20060102")
	counter, err := nextNightlyCounter(ctx, cfg.RepoRoot, cfg.OutputRoot, base, date)
	if err != nil {
		return ReleaseSubject{}, err
	}
	return releaseSubject(ChannelNightly, fmt.Sprintf("%s-nightly.%s.%d", base, date, counter), sourceRef, commit, cfg), nil
}

func (i rcIntent) deriveSubject(ctx context.Context, cfg releaseDerivationConfig) (ReleaseSubject, error) {
	sourceRef := defaultString(i.SourceRef, defaultSourceRef)
	commit, err := resolveCommit(ctx, cfg.RepoRoot, sourceRef)
	if err != nil {
		return ReleaseSubject{}, err
	}
	base, err := mkskBaseVersionAtCommit(ctx, cfg.RepoRoot, commit)
	if err != nil {
		return ReleaseSubject{}, err
	}
	version, err := nextRCVersion(ctx, cfg.RepoRoot, cfg.OutputRoot, base, commit)
	if err != nil {
		return ReleaseSubject{}, err
	}
	return releaseSubject(ChannelRC, version, sourceRef, commit, cfg), nil
}

func (i stableIntent) deriveSubject(ctx context.Context, cfg releaseDerivationConfig) (ReleaseSubject, error) {
	if strings.TrimSpace(i.FromRC) != "" {
		base, err := stableVersionFromRCTag(i.FromRC)
		if err != nil {
			return ReleaseSubject{}, err
		}
		commit, err := resolveCommit(ctx, cfg.RepoRoot, i.FromRC)
		if err != nil {
			return ReleaseSubject{}, err
		}
		return releaseSubject(ChannelStable, base, i.FromRC, commit, cfg), nil
	}
	sourceRef := defaultString(i.SourceRef, defaultSourceRef)
	commit, err := resolveCommit(ctx, cfg.RepoRoot, sourceRef)
	if err != nil {
		return ReleaseSubject{}, err
	}
	base, err := mkskBaseVersionAtCommit(ctx, cfg.RepoRoot, commit)
	if err != nil {
		return ReleaseSubject{}, err
	}
	return releaseSubject(ChannelStable, base, sourceRef, commit, cfg), nil
}

func (i exactIntent) deriveSubject(ctx context.Context, cfg releaseDerivationConfig) (ReleaseSubject, error) {
	sourceRef := defaultString(i.SourceRef, defaultSourceRef)
	commit := strings.TrimSpace(i.SourceCommit)
	if commit == "" {
		resolved, err := resolveCommit(ctx, cfg.RepoRoot, sourceRef)
		if err != nil {
			return ReleaseSubject{}, err
		}
		commit = resolved
	} else if !gitCommitRE.MatchString(commit) {
		return ReleaseSubject{}, fmt.Errorf("source commit must be a 40-character lowercase git sha")
	} else if err := verifyCommitExists(ctx, cfg.RepoRoot, commit); err != nil {
		return ReleaseSubject{}, err
	} else if resolved, err := resolveCommit(ctx, cfg.RepoRoot, sourceRef); err != nil {
		return ReleaseSubject{}, err
	} else if resolved != commit {
		return ReleaseSubject{}, fmt.Errorf("source ref %q resolves to %s, not source commit %s", sourceRef, resolved, commit)
	}
	return releaseSubject(i.Channel, i.Version, sourceRef, commit, cfg), nil
}

func releaseSubject(channel Channel, version string, sourceRef string, sourceCommit string, cfg releaseDerivationConfig) ReleaseSubject {
	return ReleaseSubject{
		Package:          packageName,
		Version:          strings.TrimSpace(version),
		Channel:          channel,
		SourceRepository: sourceRepository,
		SourceRef:        strings.TrimSpace(sourceRef),
		SourceCommit:     strings.TrimSpace(sourceCommit),
		Platform:         cfg.Platform,
		Flavor:           strings.TrimSpace(cfg.Flavor),
	}
}

func mkskBaseVersionAtCommit(ctx context.Context, repoRoot, commit string) (string, error) {
	manifest, err := gitOutput(ctx, repoRoot, "show", commit+":"+cargoManifest)
	if err != nil {
		return "", fmt.Errorf("read %s at %s: %w", cargoManifest, shortSHA(commit), err)
	}
	version, err := workspacePackageVersion(manifest)
	if err != nil {
		return "", fmt.Errorf("parse %s at %s: %w", cargoManifest, shortSHA(commit), err)
	}
	if !finalSemVerRE.MatchString(version) {
		return "", fmt.Errorf("%s [workspace.package] version must be final SemVer, got %q", cargoManifest, version)
	}
	return version, nil
}

func workspacePackageVersion(manifest string) (string, error) {
	inWorkspacePackage := false
	for _, line := range strings.Split(manifest, "\n") {
		switch {
		case workspacePackageRE.MatchString(line):
			inWorkspacePackage = true
			continue
		case inWorkspacePackage && sectionHeaderRE.MatchString(line):
			return "", fmt.Errorf("[workspace.package] version is required")
		case !inWorkspacePackage:
			continue
		}
		matches := tomlVersionRE.FindStringSubmatch(line)
		if len(matches) == 2 {
			return matches[1], nil
		}
	}
	return "", fmt.Errorf("[workspace.package] version is required")
}

func nextNightlyCounter(ctx context.Context, repoRoot, outputRoot, baseVersion string, date string) (int, error) {
	tagPattern := fmt.Sprintf("%s%s-nightly.%s.*", releaseTagPrefix, baseVersion, date)
	maxTag, err := maxCounterFromTags(ctx, repoRoot, tagPattern, func(tag string) (int, bool) {
		prefix := fmt.Sprintf("%s%s-nightly.%s.", releaseTagPrefix, baseVersion, date)
		raw, ok := strings.CutPrefix(tag, prefix)
		if !ok {
			return 0, false
		}
		counter, err := strconv.Atoi(raw)
		return counter, err == nil
	})
	if err != nil {
		return 0, err
	}
	maxLocal, err := maxCounterFromLocalBuilds(outputRoot, regexp.MustCompile(`^build-nightly-`+regexp.QuoteMeta(baseVersion)+`-nightly\.`+regexp.QuoteMeta(date)+`\.(\d+)-`))
	if err != nil {
		return 0, err
	}
	return maxInt(maxTag, maxLocal) + 1, nil
}

func nextRCVersion(ctx context.Context, repoRoot, outputRoot, baseVersion string, sourceCommit string) (string, error) {
	tagPattern := fmt.Sprintf("%s%s-rc.*", releaseTagPrefix, baseVersion)
	maxTag := 0
	maxSameCommit := 0
	tags, err := gitTagList(ctx, repoRoot, tagPattern)
	if err != nil {
		return "", err
	}
	for _, tag := range tags {
		base, counter, ok := parseRCTag(tag)
		if !ok || base != baseVersion {
			continue
		}
		maxTag = maxInt(maxTag, counter)
		tagCommit, err := resolveCommit(ctx, repoRoot, tag)
		if err != nil {
			return "", err
		}
		if tagCommit == sourceCommit {
			maxSameCommit = maxInt(maxSameCommit, counter)
		}
	}
	if maxSameCommit > 0 {
		return fmt.Sprintf("%s-rc.%d", baseVersion, maxSameCommit), nil
	}
	maxLocal, err := maxCounterFromLocalBuilds(outputRoot, regexp.MustCompile(`^build-rc-`+regexp.QuoteMeta(baseVersion)+`-rc\.(\d+)-`))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-rc.%d", baseVersion, maxInt(maxTag, maxLocal)+1), nil
}

func stableVersionFromRCTag(tag string) (string, error) {
	base, _, ok := parseRCTag(strings.TrimSpace(tag))
	if !ok {
		return "", fmt.Errorf("--from-rc must match %sMAJOR.MINOR.PATCH-rc.N", releaseTagPrefix)
	}
	return base, nil
}

func parseRCTag(tag string) (base string, counter int, ok bool) {
	matches := rcTagRE.FindStringSubmatch(tag)
	if len(matches) != 3 {
		return "", 0, false
	}
	counter, err := strconv.Atoi(matches[2])
	if err != nil || counter <= 0 {
		return "", 0, false
	}
	return matches[1], counter, true
}

func maxCounterFromTags(ctx context.Context, repoRoot, pattern string, parse func(string) (int, bool)) (int, error) {
	tags, err := gitTagList(ctx, repoRoot, pattern)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, tag := range tags {
		counter, ok := parse(tag)
		if ok {
			max = maxInt(max, counter)
		}
	}
	return max, nil
}

func gitTagList(ctx context.Context, repoRoot, pattern string) ([]string, error) {
	out, err := gitOutput(ctx, repoRoot, "tag", "--list", pattern)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(out), nil
}

func maxCounterFromLocalBuilds(outputRoot string, pattern *regexp.Regexp) (int, error) {
	dir := filepath.Join(outputRoot, packageName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read local release output root %s: %w", dir, err)
	}
	max := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		matches := pattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}
		counter, err := strconv.Atoi(matches[1])
		if err == nil {
			max = maxInt(max, counter)
		}
	}
	return max, nil
}

func verifyCommitExists(ctx context.Context, repoRoot, commit string) error {
	if _, err := gitOutput(ctx, repoRoot, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return fmt.Errorf("source commit %s is not present in repository history: %w", commit, err)
	}
	return nil
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
