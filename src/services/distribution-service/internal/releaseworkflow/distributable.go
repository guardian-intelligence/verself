package releaseworkflow

import (
	"context"
	"fmt"
	"strings"
)

const (
	EvidencePolicyStandardCLIV1 = "standard-cli-v1"
)

type Distributable struct {
	Package            string
	SourceRepo         string
	ReleaseTarget      string
	ReleaseToolsTarget string
	EvidencePolicy     string
}

var distributables = map[string]Distributable{
	PackageMksk: {
		Package:            PackageMksk,
		SourceRepo:         "https://github.com/guardian-intelligence/verself.git",
		ReleaseTarget:      "//src/make-skill/release:mksk-release",
		ReleaseToolsTarget: "//src/make-skill/release:release_tools_artifact",
		EvidencePolicy:     EvidencePolicyStandardCLIV1,
	},
}

func LookupDistributable(packageName string) (Distributable, error) {
	packageName = strings.TrimSpace(packageName)
	if !packagePattern.MatchString(packageName) {
		return Distributable{}, fmt.Errorf("%w: package name must be lowercase kebab-case", ErrInvalidInput)
	}
	dist, ok := distributables[packageName]
	if !ok {
		return Distributable{}, fmt.Errorf("%w: unsupported package %q", ErrInvalidInput, packageName)
	}
	for name, value := range map[string]string{
		"package":         dist.Package,
		"source repo":     dist.SourceRepo,
		"release target":  dist.ReleaseTarget,
		"release tools":   dist.ReleaseToolsTarget,
		"evidence policy": dist.EvidencePolicy,
	} {
		if strings.TrimSpace(value) == "" {
			return Distributable{}, fmt.Errorf("%w: distributable %s is missing %s", ErrInvalidInput, packageName, name)
		}
	}
	if dist.Package != packageName {
		return Distributable{}, fmt.Errorf("%w: distributable %s registry key does not match package %s", ErrInvalidInput, packageName, dist.Package)
	}
	return dist, nil
}

func ResolvePinnedSource(ctx context.Context, executor CommandExecutor, packageName string, source FloatingSource, gitBinary string) (PinnedSource, error) {
	dist, err := LookupDistributable(packageName)
	if err != nil {
		return PinnedSource{}, err
	}
	ref := strings.TrimSpace(source.Ref)
	if err := validateSourceRef(ref); err != nil {
		return PinnedSource{}, err
	}
	if gitCommitPattern.MatchString(ref) {
		return PinnedSource{Ref: ref, Commit: ref}, nil
	}
	gitBinary = defaultString(gitBinary, DefaultGitBin)
	result, err := executor.Run(ctx, Command{
		Name: gitBinary,
		Args: append([]string{"ls-remote", dist.SourceRepo}, lsRemotePatterns(ref)...),
	})
	if err != nil {
		return PinnedSource{}, fmt.Errorf("resolve source ref %s: %w", ref, err)
	}
	commit, err := selectRemoteCommit(ref, result.Stdout)
	if err != nil {
		return PinnedSource{}, err
	}
	return PinnedSource{Ref: ref, Commit: commit}, nil
}
