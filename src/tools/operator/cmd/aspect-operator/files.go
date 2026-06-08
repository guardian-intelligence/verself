package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func loadSiteFacts(repoRoot, site string) (cue.Value, string, error) {
	path := siteFactsPath(repoRoot, site)
	b, err := os.ReadFile(path)
	if err != nil {
		return cue.Value{}, "", fmt.Errorf("read %s: %w", path, err)
	}
	ctx := cuecontext.New()
	value := ctx.CompileBytes(b, cue.Filename(path)).FillPath(cue.ParsePath("site"), normalizedSite(site))
	if err := value.Validate(cue.Concrete(true)); err != nil {
		return cue.Value{}, "", fmt.Errorf("%s: site facts must be concrete: %w", path, err)
	}
	got, err := siteFactString(value, cue.ParsePath("site"), true)
	if err != nil {
		return cue.Value{}, "", fmt.Errorf("%s: site is required: %w", path, err)
	}
	if got != normalizedSite(site) {
		return cue.Value{}, "", fmt.Errorf("%s: site=%q does not match selected site %q", path, got, normalizedSite(site))
	}
	return value, path, nil
}

func siteFactString(root cue.Value, path cue.Path, required bool) (string, error) {
	value := root.LookupPath(path)
	if !value.Exists() {
		if required {
			return "", fmt.Errorf("missing %s", path.String())
		}
		return "", nil
	}
	text, err := value.String()
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if required && text == "" {
		return "", fmt.Errorf("empty %s", path.String())
	}
	return text, nil
}

func siteFactsPath(repoRoot, site string) string {
	return filepath.Join(repoRoot, "src", "guardian-specification", "examples", normalizedSite(site), "facts.cue")
}

func normalizedSite(site string) string {
	site = strings.TrimSpace(site)
	if site == "" {
		return "prod"
	}
	return site
}
