package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	defaultNPMRegistry         = "https://registry.npmjs.org"
	defaultGitHubRepository    = "guardian-intelligence/verself-sh"
	releaseCheckStatusOK       = "ok"
	releaseCheckStatusWarn     = "warn"
	releaseCheckStatusBlocked  = "blocked"
	releaseCheckStatusInfo     = "info"
	trustedPublisherRunnerNote = "npm trusted publishing supports GitHub-hosted, GitLab.com shared, and CircleCI cloud runners; self-hosted runners are not supported"
)

var npmSemVerRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

type packageJSON struct {
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Private       bool            `json:"private"`
	PublishConfig publishConfig   `json:"publishConfig"`
	Repository    json.RawMessage `json:"repository"`
}

type publishConfig struct {
	Access   string `json:"access"`
	Registry string `json:"registry"`
}

type npmRegistryPackage struct {
	Name     string                     `json:"name"`
	DistTags map[string]string          `json:"dist-tags"`
	Versions map[string]json.RawMessage `json:"versions"`
}

type npmRegistryProbe struct {
	PackageURL string
	Found      bool
	Metadata   npmRegistryPackage
}

type releaseProbeRow struct {
	Check  string
	Status string
	Detail string
}

func cmdRelease(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("release: missing subcommand (try `npm-probe`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "npm-probe":
		return cmdReleaseNPMProbe(rest)
	default:
		return fmt.Errorf("release: unknown subcommand: %s", sub)
	}
}

func cmdReleaseNPMProbe(args []string) error {
	fs := flagSet("release npm-probe")
	packageJSONPath := fs.String("package-json", "src/websites/packages/sdk/package.json", "Package manifest to inspect")
	registry := fs.String("registry", defaultNPMRegistry, "npm registry base URL")
	githubRepository := fs.String("github-repository", defaultGitHubRepository, "GitHub owner/repo expected by npm trusted publishing")
	format := fs.String("format", "table", "Output format: table | json | csv | tsv")
	requirePublishable := fs.Bool("require-publishable", false, "Fail when local manifest fields block publishing")
	timeout := fs.Duration("timeout", 15*time.Second, "Registry probe timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pkg, err := readPackageJSON(*packageJSONPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	probe, err := fetchNPMRegistryPackage(ctx, http.DefaultClient, *registry, pkg.Name)
	if err != nil {
		return err
	}
	rows, blockers := buildNPMProbeRows(pkg, *packageJSONPath, *registry, *githubRepository, probe)
	if err := printReleaseProbeRows(os.Stdout, rows, *format); err != nil {
		return err
	}
	if *requirePublishable && blockers > 0 {
		return fmt.Errorf("npm probe found %d publish blocker(s)", blockers)
	}
	return nil
}

func readPackageJSON(path string) (packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageJSON{}, fmt.Errorf("read package json %s: %w", path, err)
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, fmt.Errorf("parse package json %s: %w", path, err)
	}
	if strings.TrimSpace(pkg.Name) == "" {
		return packageJSON{}, fmt.Errorf("package json %s: name is required", path)
	}
	if strings.TrimSpace(pkg.Version) == "" {
		return packageJSON{}, fmt.Errorf("package json %s: version is required", path)
	}
	return pkg, nil
}

func fetchNPMRegistryPackage(ctx context.Context, client *http.Client, registry, packageName string) (npmRegistryProbe, error) {
	packageURL, err := npmPackageMetadataURL(registry, packageName)
	if err != nil {
		return npmRegistryProbe{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, packageURL, nil)
	if err != nil {
		return npmRegistryProbe{}, fmt.Errorf("build npm registry request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "verself-release-probe/dev")
	resp, err := client.Do(req)
	if err != nil {
		return npmRegistryProbe{}, fmt.Errorf("query npm registry %s: %w", packageURL, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var metadata npmRegistryPackage
		if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
			return npmRegistryProbe{}, fmt.Errorf("decode npm registry metadata for %s: %w", packageName, err)
		}
		return npmRegistryProbe{PackageURL: packageURL, Found: true, Metadata: metadata}, nil
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, resp.Body)
		return npmRegistryProbe{PackageURL: packageURL}, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return npmRegistryProbe{}, fmt.Errorf("npm registry %s returned %s: %s", packageURL, resp.Status, strings.TrimSpace(string(body)))
	}
}

func npmPackageMetadataURL(registry, packageName string) (string, error) {
	if strings.TrimSpace(packageName) == "" {
		return "", fmt.Errorf("npm package name is required")
	}
	parsed, err := url.Parse(registry)
	if err != nil {
		return "", fmt.Errorf("parse npm registry URL %q: %w", registry, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("npm registry URL must be absolute: %s", registry)
	}
	return strings.TrimRight(parsed.String(), "/") + "/" + url.PathEscape(packageName), nil
}

func buildNPMProbeRows(pkg packageJSON, packageJSONPath, registry, githubRepository string, probe npmRegistryProbe) ([]releaseProbeRow, int) {
	var rows []releaseProbeRow
	blockers := 0
	add := func(check, status, detail string) {
		if status == releaseCheckStatusBlocked {
			blockers++
		}
		rows = append(rows, releaseProbeRow{Check: check, Status: status, Detail: detail})
	}

	add("package_json", releaseCheckStatusOK, fmt.Sprintf("%s declares %s@%s", packageJSONPath, pkg.Name, pkg.Version))
	if npmSemVerRE.MatchString(pkg.Version) {
		add("package_version", releaseCheckStatusOK, "version is npm SemVer-compatible")
	} else {
		add("package_version", releaseCheckStatusBlocked, "version must be a SemVer string before npm publish")
	}
	if pkg.Private {
		add("publishable_private_flag", releaseCheckStatusBlocked, "package.json has private=true; npm publish will refuse this package")
	} else {
		add("publishable_private_flag", releaseCheckStatusOK, "package is not marked private")
	}
	if strings.EqualFold(pkg.PublishConfig.Access, "public") {
		add("publish_config_access", releaseCheckStatusOK, "publishConfig.access is public")
	} else {
		add("publish_config_access", releaseCheckStatusWarn, "set publishConfig.access=public or pass npm publish --access public for scoped packages")
	}
	if registryUsesHTTPS(registry) {
		add("registry_transport", releaseCheckStatusOK, "registry URL uses HTTPS")
	} else {
		add("registry_transport", releaseCheckStatusBlocked, "npm registry URL must use HTTPS")
	}
	if pkg.PublishConfig.Registry != "" && strings.TrimRight(pkg.PublishConfig.Registry, "/") != strings.TrimRight(registry, "/") {
		add("publish_config_registry", releaseCheckStatusWarn, fmt.Sprintf("publishConfig.registry=%s differs from probe registry %s", pkg.PublishConfig.Registry, registry))
	}
	addTrustedPublisherRepositoryCheck(add, pkg, githubRepository)
	add("trusted_publisher_runner", releaseCheckStatusInfo, trustedPublisherRunnerNote)

	if !probe.Found {
		add("npm_registry_package", releaseCheckStatusOK, fmt.Sprintf("%s returned 404; package name is not currently visible on this registry", probe.PackageURL))
		add("npm_registry_version", releaseCheckStatusOK, fmt.Sprintf("%s@%s is not published", pkg.Name, pkg.Version))
		return rows, blockers
	}
	add("npm_registry_package", releaseCheckStatusOK, fmt.Sprintf("%s exists with %d version(s)", probe.Metadata.Name, len(probe.Metadata.Versions)))
	if published, ok := probe.Metadata.Versions[pkg.Version]; ok && published != nil {
		add("npm_registry_version", releaseCheckStatusBlocked, fmt.Sprintf("%s@%s is already published; npm versions are immutable", pkg.Name, pkg.Version))
	} else {
		add("npm_registry_version", releaseCheckStatusOK, fmt.Sprintf("%s@%s is available", pkg.Name, pkg.Version))
	}
	if latest := probe.Metadata.DistTags["latest"]; latest != "" {
		add("npm_registry_latest", releaseCheckStatusInfo, latest)
	}
	return rows, blockers
}

func registryUsesHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func addTrustedPublisherRepositoryCheck(add func(string, string, string), pkg packageJSON, githubRepository string) {
	repoURL := packageRepositoryURL(pkg.Repository)
	if repoURL == "" {
		add("trusted_publisher_repository", releaseCheckStatusBlocked, "repository.url is missing; npm trusted publishing requires an exact GitHub repository URL match")
		return
	}
	ownerRepo := normalizeGitHubOwnerRepo(repoURL)
	if ownerRepo == "" {
		add("trusted_publisher_repository", releaseCheckStatusBlocked, fmt.Sprintf("repository.url must point at github.com/%s for npm trusted publishing: %s", githubRepository, repoURL))
		return
	}
	if githubRepository != "" && !strings.EqualFold(ownerRepo, githubRepository) {
		add("trusted_publisher_repository", releaseCheckStatusBlocked, fmt.Sprintf("repository.url points at %s, expected %s", ownerRepo, githubRepository))
		return
	}
	add("trusted_publisher_repository", releaseCheckStatusOK, repoURL)
}

func packageRepositoryURL(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		return strings.TrimSpace(stringValue)
	}
	var objectValue struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &objectValue); err != nil {
		return ""
	}
	return strings.TrimSpace(objectValue.URL)
}

func normalizeGitHubOwnerRepo(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "git+")
	clean = strings.TrimSuffix(clean, ".git")
	if strings.HasPrefix(clean, "git@github.com:") {
		return strings.TrimPrefix(clean, "git@github.com:")
	}
	parsed, err := url.Parse(clean)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func printReleaseProbeRows(w io.Writer, rows []releaseProbeRow, format string) error {
	table := opruntime.Table{
		Headers: []string{"check", "status", "detail"},
		Rows:    make([][]string, 0, len(rows)),
	}
	for _, row := range rows {
		table.Rows = append(table.Rows, []string{row.Check, row.Status, row.Detail})
	}
	return printTableFormat(w, table, format)
}
