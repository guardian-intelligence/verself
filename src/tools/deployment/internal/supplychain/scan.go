package supplychain

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ScanOptions struct {
	RepoRoot string
}

func Scan(_ context.Context, opts ScanOptions) (Report, error) {
	root := opts.RepoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Report{}, fmt.Errorf("supplychain: cwd: %w", err)
		}
		root = cwd
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("supplychain: absolute repo root: %w", err)
	}
	var findings []Finding
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldScanFile(rel) {
			return nil
		}
		fileFindings, err := scanFile(path, rel)
		if err != nil {
			return err
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	SortFindings(findings)
	return Report{Findings: findings}, nil
}

func shouldSkipDir(rel string) bool {
	if rel == "." {
		return false
	}
	base := filepath.Base(rel)
	switch base {
	case ".git", ".cache", "node_modules", "bazel-bin", "bazel-out", "bazel-testlogs", "bazel-verself-sh":
		return true
	}
	if strings.HasPrefix(rel, "docs/references") ||
		strings.HasPrefix(rel, "artifacts") ||
		strings.HasPrefix(rel, "smoke-artifacts") {
		return true
	}
	return false
}

func shouldScanFile(rel string) bool {
	switch rel {
	case "MODULE.bazel", "MODULE.aspect":
		return true
	}
	if isBootstrapScript(rel) {
		return true
	}
	if strings.HasPrefix(rel, ".aspect/") {
		return strings.HasSuffix(rel, ".axl")
	}
	if !strings.HasPrefix(rel, "src/") {
		return false
	}
	if rel == "src/frontends/viteplus-monorepo/.npmrc" {
		return true
	}
	if strings.HasPrefix(rel, "src/tools/deployment/internal/supplychain/") {
		return false
	}
	if strings.HasPrefix(rel, "src/host/supply-chain/") {
		return false
	}
	if rel == "src/substrate/vm-orchestrator/guest-images/substrate/build_substrate.go" {
		return true
	}
	ext := filepath.Ext(rel)
	switch ext {
	case ".bazel", ".bzl", ".sh", ".yml", ".yaml", ".json", ".toml":
		return true
	default:
		return false
	}
}

func scanFile(path, rel string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("supplychain: open %s: %w", rel, err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("supplychain: read %s: %w", rel, err)
	}
	text := string(raw)
	var findings []Finding
	if strings.Contains(text, "http_file(") || strings.Contains(text, "http_archive(") {
		findings = append(findings, scanBazelRepoRules(rel, text)...)
	}
	if isBootstrapScript(rel) {
		findings = append(findings, scanShellURLVars(rel, text)...)
	}
	if rel == "src/frontends/viteplus-monorepo/pnpm-workspace.yaml" {
		findings = append(findings, scanPnpmSettings(rel, text)...)
	}
	if rel == "src/frontends/viteplus-monorepo/.npmrc" {
		findings = append(findings, scanNpmrcSettings(rel, text)...)
	}
	if strings.HasSuffix(rel, "catalog.yml") {
		findings = append(findings, scanCatalogURLs(rel, text)...)
		return dedupeFindings(findings), nil
	}
	findings = append(findings, scanLines(rel, text)...)
	return dedupeFindings(findings), nil
}

var (
	bazelRuleStartRe = regexp.MustCompile(`\b(http_file|http_archive)\s*\(`)
	bazelNameRe      = regexp.MustCompile(`\bname\s*=\s*"([^"]+)"`)
	bazelURLRe       = regexp.MustCompile(`\burl\s*=\s*"([^"]+)"`)
	bazelSHARe       = regexp.MustCompile(`\bsha256\s*=\s*"([0-9a-fA-F]+)"`)
)

func scanBazelRepoRules(rel, text string) []Finding {
	lines := strings.Split(text, "\n")
	var findings []Finding
	for i := 0; i < len(lines); i++ {
		start := bazelRuleStartRe.FindStringSubmatch(lines[i])
		if start == nil {
			continue
		}
		startLine := i + 1
		var block []string
		depth := 0
		for j := i; j < len(lines); j++ {
			line := lines[j]
			block = append(block, line)
			depth += strings.Count(line, "(")
			depth -= strings.Count(line, ")")
			if depth <= 0 && j > i {
				i = j
				break
			}
		}
		joined := strings.Join(block, "\n")
		name := firstSubmatch(bazelNameRe, joined)
		rawURL := firstSubmatch(bazelURLRe, joined)
		if name == "" || rawURL == "" {
			continue
		}
		digest := normalizeDigest(firstSubmatch(bazelSHARe, joined))
		kind := "bazel_" + start[1]
		findings = append(findings, finding(rel, uint32(startLine), kind, classifySurface(rel, kind, name), name, rawURL, digest, strings.TrimSpace(block[0])))
	}
	return findings
}

var (
	yamlURLRe       = regexp.MustCompile(`^\s*([A-Za-z0-9_]*url):\s*["']?([^"'\s]+)["']?\s*$`)
	yamlSHARe       = regexp.MustCompile(`^\s*([A-Za-z0-9_]*sha256):\s*["']?([0-9a-fA-F]+)["']?\s*$`)
	yamlRootKeyRe   = regexp.MustCompile(`^([A-Za-z0-9_.-]+):\s*$`)
	yamlChildKeyRe  = regexp.MustCompile(`^  ([A-Za-z0-9_.-]+):\s*$`)
	shellSHARe      = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)_sha256=["']?([0-9a-fA-F]+)["']?\s*$`)
	shellURLRe      = regexp.MustCompile(`^\s*([A-Za-z0-9_]+?)(?:_install)?_url=["'](https?://[^"']+)["']\s*$`)
	aptGetUpdateRe  = regexp.MustCompile(`\bapt-get\s+update\b`)
	aptGetInstallRe = regexp.MustCompile(`\bapt-get\s+install\b`)
	npmInstallRe    = regexp.MustCompile(`\bnpm\s+install\b`)
	uvToolRe        = regexp.MustCompile(`\buv\s+tool\s+install\b`)
	uvxFromRe       = regexp.MustCompile(`\buvx\b.*\b--from\b`)
	goInstallRe     = regexp.MustCompile(`\bgo\s+install\s+[^#\n]+@`)
	pipInstallRe    = regexp.MustCompile(`\bpip(?:3)?\s+install\b`)
	curlRe          = regexp.MustCompile(`\bcurl\b`)
	wgetRe          = regexp.MustCompile(`\bwget\b`)
	pnpmScalarRe    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*):\s*([^#\s]+)\s*$`)
	pnpmAllowRe     = regexp.MustCompile(`^  ([A-Za-z0-9@/_.-]+):\s*(true|false)\s*$`)
	pnpmListRe      = regexp.MustCompile(`^  -\s+([A-Za-z0-9@/_.|\s-]+)\s*$`)
	npmrcRegistryRe = regexp.MustCompile(`^\s*registry\s*=\s*([^#\s]+)\s*$`)
	registryRe      = regexp.MustCompile(`https?://[^"'\s]*registry\.npmjs\.org[^"'\s]*|registry\.npmjs\.org`)
	githubReleaseRe = regexp.MustCompile(`https://(?:github\.com|codeberg\.org|code\.forgejo\.org)/[^"'\s]+/releases/download/[^"'\s]+`)
	httpURLRe       = regexp.MustCompile(`https?://[^"'\s)>,]+`)
)

func scanPnpmSettings(rel, text string) []Finding {
	required := map[string]bool{
		"minimumReleaseAge":           true,
		"strictDepBuilds":             true,
		"dangerouslyAllowAllBuilds":   true,
		"verifyDepsBeforeRun":         true,
		"packageManagerStrict":        true,
		"packageManagerStrictVersion": true,
	}
	lines := strings.Split(text, "\n")
	var findings []Finding
	inAllowBuilds := false
	inOnlyBuiltDependencies := false
	for idx, line := range lines {
		lineNo := uint32(idx + 1)
		if strings.TrimSpace(line) == "allowBuilds:" {
			inAllowBuilds = true
			inOnlyBuiltDependencies = false
			continue
		}
		if strings.TrimSpace(line) == "onlyBuiltDependencies:" {
			inAllowBuilds = false
			inOnlyBuiltDependencies = true
			continue
		}
		if inAllowBuilds {
			if strings.HasPrefix(line, "  ") {
				if m := pnpmAllowRe.FindStringSubmatch(line); m != nil {
					artifact := "pnpm.allowBuilds." + m[1]
					findings = append(findings, finding(rel, lineNo, "pnpm_setting", "developer-only", artifact, "", "value:"+m[2], strings.TrimSpace(line)))
				}
				continue
			}
			inAllowBuilds = false
		}
		if inOnlyBuiltDependencies {
			if strings.HasPrefix(line, "  ") {
				if m := pnpmListRe.FindStringSubmatch(line); m != nil {
					artifact := "pnpm.onlyBuiltDependencies." + strings.TrimSpace(m[1])
					findings = append(findings, finding(rel, lineNo, "pnpm_setting", "developer-only", artifact, "", "listed", strings.TrimSpace(line)))
				}
				continue
			}
			inOnlyBuiltDependencies = false
		}
		if m := pnpmScalarRe.FindStringSubmatch(line); m != nil && required[m[1]] {
			artifact := "pnpm." + m[1]
			findings = append(findings, finding(rel, lineNo, "pnpm_setting", "developer-only", artifact, "", "value:"+m[2], strings.TrimSpace(line)))
		}
	}
	return findings
}

func scanNpmrcSettings(rel, text string) []Finding {
	lines := strings.Split(text, "\n")
	var findings []Finding
	for idx, line := range lines {
		if m := npmrcRegistryRe.FindStringSubmatch(line); m != nil {
			raw := strings.TrimRight(m[1], "/") + "/"
			findings = append(findings, finding(rel, uint32(idx+1), "registry_url", classifySurface(rel, "registry_url", registryArtifact(raw)), registryArtifact(raw), raw, "", strings.TrimSpace(line)))
		}
	}
	return findings
}

func scanShellURLVars(rel, text string) []Finding {
	lines := strings.Split(text, "\n")
	digests := map[string]string{}
	var findings []Finding
	for idx, line := range lines {
		lineNo := uint32(idx + 1)
		if m := shellSHARe.FindStringSubmatch(line); m != nil {
			digests[m[1]] = normalizeDigest(m[2])
			continue
		}
		if m := shellURLRe.FindStringSubmatch(line); m != nil {
			artifact := m[1]
			findings = append(findings, finding(rel, lineNo, "bootstrap_url", classifySurface(rel, "bootstrap_url", artifact), artifact, m[2], digests[artifact], strings.TrimSpace(line)))
		}
	}
	return findings
}

func scanCatalogURLs(rel, text string) []Finding {
	lines := strings.Split(text, "\n")
	var findings []Finding
	section := ""
	child := ""
	type urlHit struct {
		key  string
		line uint32
		url  string
	}
	urls := map[string]urlHit{}
	digests := map[string]string{}
	flush := func() {
		if section == "" || child == "" {
			return
		}
		for prefix, hit := range urls {
			artifact := section + "." + child
			if prefix != "" {
				artifact += "." + prefix
			}
			digest := digests[prefix]
			findings = append(findings, finding(rel, hit.line, "catalog_url", classifyCatalogSurface(section), artifact, hit.url, normalizeDigest(digest), hit.key))
		}
	}
	for idx, line := range lines {
		lineNo := uint32(idx + 1)
		if m := yamlRootKeyRe.FindStringSubmatch(line); m != nil {
			flush()
			section = m[1]
			child = ""
			urls = map[string]urlHit{}
			digests = map[string]string{}
			continue
		}
		if m := yamlChildKeyRe.FindStringSubmatch(line); m != nil {
			flush()
			child = m[1]
			urls = map[string]urlHit{}
			digests = map[string]string{}
			continue
		}
		if child == "" {
			continue
		}
		if m := yamlURLRe.FindStringSubmatch(line); m != nil {
			urls[urlPrefix(m[1])] = urlHit{key: strings.TrimSpace(m[1]), line: lineNo, url: m[2]}
			continue
		}
		if m := yamlSHARe.FindStringSubmatch(line); m != nil {
			digests[shaPrefix(m[1])] = m[2]
		}
	}
	flush()
	return findings
}

func urlPrefix(key string) string {
	return strings.TrimSuffix(key, "url")
}

func shaPrefix(key string) string {
	return strings.TrimSuffix(key, "sha256")
}

func classifyCatalogSurface(section string) string {
	switch section {
	case "topology_guest_versions":
		return "guest-rootfs"
	case "topology_dev_tools":
		return "developer-only"
	default:
		return "host-bootstrap"
	}
}

func scanLines(rel, text string) []Finding {
	lines := strings.Split(text, "\n")
	var findings []Finding
	for idx, line := range lines {
		lineNo := uint32(idx + 1)
		findings = append(findings, commandFindingsForLine(rel, lineNo, line)...)
	}
	return findings
}

func commandFindingsForLine(rel string, lineNo uint32, line string) []Finding {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return nil
	}
	var out []Finding
	addCommand := func(kind, artifact, rawURL string) {
		out = append(out, finding(rel, lineNo, kind, classifySurface(rel, kind, artifact), artifact, rawURL, "", trimmed))
	}
	if aptGetUpdateRe.MatchString(line) {
		addCommand("apt_get_update", "apt-get-update", firstURL(line))
	}
	if aptGetInstallRe.MatchString(line) {
		addCommand("apt_get_install", "apt-get-install", firstURL(line))
	}
	if npmInstallRe.MatchString(line) {
		addCommand("npm_install", npmInstallArtifact(line), firstURL(line))
	}
	if uvToolRe.MatchString(line) {
		addCommand("uv_tool_install", uvInstallArtifact(line), firstURL(line))
	}
	if uvxFromRe.MatchString(line) {
		addCommand("uvx_from", uvxArtifact(line), firstURL(line))
	}
	if goInstallRe.MatchString(line) {
		addCommand("go_install", goInstallArtifact(line), firstURL(line))
	}
	if pipInstallRe.MatchString(line) {
		addCommand("pip_install", "pip-install", firstURL(line))
	}
	if shouldCaptureNetworkCommand(rel, line) {
		if curlRe.MatchString(line) {
			addCommand("curl_fetch", "curl", firstURL(line))
		}
		if wgetRe.MatchString(line) {
			addCommand("wget_fetch", "wget", firstURL(line))
		}
	}
	for _, raw := range registryRe.FindAllString(line, -1) {
		addCommand("registry_url", registryArtifact(raw), normalizeRegistry(raw))
	}
	if strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml") {
		for _, raw := range githubReleaseRe.FindAllString(line, -1) {
			addCommand("direct_release_url", artifactFromURL(raw), raw)
		}
	}
	return out
}

func shouldCaptureNetworkCommand(rel, line string) bool {
	_ = rel
	if strings.Contains(line, "curl ") || strings.Contains(line, "wget ") {
		return strings.Contains(line, "http://") || strings.Contains(line, "https://")
	}
	return false
}

func firstSubmatch(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func firstURL(line string) string {
	return httpURLRe.FindString(line)
}

func normalizeDigest(d string) string {
	if d == "" {
		return ""
	}
	if strings.HasPrefix(d, "sha256:") {
		return strings.ToLower(d)
	}
	return "sha256:" + strings.ToLower(d)
}

func normalizeRegistry(raw string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "https://" + raw
}

func registryArtifact(raw string) string {
	if strings.Contains(raw, "registry.npmjs.org") {
		return "npmjs-registry"
	}
	return "registry"
}

func artifactFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "artifact"
	}
	base := filepath.Base(parsed.Path)
	if base == "." || base == "/" || base == "" {
		return parsed.Host
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func npmInstallArtifact(line string) string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field == "install" && i+1 < len(fields) {
			next := strings.Trim(fields[i+1], "'\"")
			if !strings.HasPrefix(next, "-") {
				return strings.TrimPrefix(next, "@")
			}
		}
	}
	return "npm-install"
}

func uvInstallArtifact(line string) string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field == "install" && i+1 < len(fields) {
			next := strings.Trim(fields[i+1], "'\"")
			if !strings.HasPrefix(next, "-") {
				return next
			}
		}
	}
	return "uv-tool-install"
}

func uvxArtifact(line string) string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field == "--from" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "'\"")
		}
	}
	return "uvx-from"
}

func goInstallArtifact(line string) string {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field == "install" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "'\"")
		}
	}
	return "go-install"
}

func classifySurface(rel, kind, artifact string) string {
	switch {
	case rel == "src/frontends/viteplus-monorepo/.npmrc":
		return "build-time"
	case strings.HasPrefix(rel, "src/substrate/vm-orchestrator/guest-images/"):
		return "guest-rootfs"
	case strings.Contains(rel, "verdaccio/templates/"):
		return "runtime"
	case strings.Contains(rel, "dev-tools") || strings.Contains(rel, "uv_tools") || strings.Contains(artifact, "dev_tool"):
		return "developer-only"
	case isBootstrapScript(rel):
		return "host-bootstrap"
	case strings.Contains(rel, "host") || strings.Contains(artifact, "server_tool"):
		return "host-bootstrap"
	case strings.Contains(kind, "registry"):
		return "runtime"
	default:
		return "build-time"
	}
}

func isBootstrapScript(rel string) bool {
	return strings.HasPrefix(rel, "scripts/bootstrap-") && !strings.Contains(strings.TrimPrefix(rel, "scripts/"), "/")
}

func finding(rel string, line uint32, kind, surface, artifact, upstreamURL, digest, evidence string) Finding {
	if artifact == "" {
		artifact = artifactFromURL(upstreamURL)
	}
	if artifact == "" {
		artifact = kind
	}
	return Finding{
		ID:          MakeID(rel, line, kind, artifact),
		SourcePath:  rel,
		Line:        line,
		SourceKind:  kind,
		Surface:     surface,
		Artifact:    artifact,
		UpstreamURL: upstreamURL,
		Digest:      digest,
		Evidence:    evidence,
	}
}

func dedupeFindings(in []Finding) []Finding {
	seen := map[string]bool{}
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		key := strings.Join([]string{f.SourcePath, fmt.Sprint(f.Line), f.SourceKind, f.Artifact, f.UpstreamURL, f.Digest}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

func ReadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("scan %s", path), err)
	}
	return lines, nil
}
