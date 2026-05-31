package deploycontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	nameRE      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	secretRE    = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_]+)+$`)
	modeRE      = regexp.MustCompile(`^0[0-7]{3}$`)
	durationRE  = regexp.MustCompile(`^[1-9][0-9]*(ms|s|m|h)$`)
	siteNameRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	apiKeyRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	allowedRoot = map[string]bool{
		"src/infrastructure-components": true,
		"src/services":                  true,
		"src/viteplus-monorepo/apps":    true,
	}
)

type Report struct {
	DeployFiles      int `json:"deploy_files"`
	IntegrationFiles int `json:"integration_files"`
	PostgresFiles    int `json:"postgres_files"`
	RuntimeSecrets   int `json:"runtime_secrets"`
	CredstoreFiles   int `json:"credstore_files"`
	PublicRoutes     int `json:"public_routes"`
	PublicAPIs       int `json:"public_apis"`
	ClickHouseCreds  int `json:"clickhouse_credentials"`
	Gates            int `json:"promotion_gates"`
	NomadSpecs       int `json:"nomad_specs"`
}

type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

type Validator struct {
	root   string
	report Report
	errs   []ValidationError
	seen   seenClaims
}

type seenClaims struct {
	postgresDatabases map[string]string
	postgresOwners    map[string]string
	replicationRoles  map[string]string
	publicRouteHosts  map[string]string
	publicAPIKeys     map[string]string
	runtimeSecrets    map[string]string
	credstorePaths    map[string]string
	clickhouseCreds   map[string]string
	gateNames         map[string]string
	peerMappings      []postgresPeerClaim
}

type postgresPeerClaim struct {
	path          string
	systemUser    string
	pgUser        string
	purpose       string
	justification string
}

func ValidateRepo(root string) (Report, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Report{}, errors.New("repo root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("resolve repo root: %w", err)
	}
	v := &Validator{
		root: abs,
		seen: seenClaims{
			postgresDatabases: map[string]string{},
			postgresOwners:    map[string]string{},
			replicationRoles:  map[string]string{},
			publicRouteHosts:  map[string]string{},
			publicAPIKeys:     map[string]string{},
			runtimeSecrets:    map[string]string{},
			credstorePaths:    map[string]string{},
			clickhouseCreds:   map[string]string{},
			gateNames:         map[string]string{},
		},
	}
	if err := v.walkDeployFiles(); err != nil {
		return Report{}, err
	}
	v.validatePostgresPeerMappings()
	if err := v.walkIntegrationCatalogs(); err != nil {
		return Report{}, err
	}
	if err := v.walkSiteSecrets(); err != nil {
		return Report{}, err
	}
	if err := v.walkNomadSpecs(); err != nil {
		return Report{}, err
	}
	if len(v.errs) > 0 {
		sort.Slice(v.errs, func(i, j int) bool {
			if v.errs[i].Path != v.errs[j].Path {
				return v.errs[i].Path < v.errs[j].Path
			}
			return v.errs[i].Message < v.errs[j].Message
		})
		return v.report, ErrorList(v.errs)
	}
	return v.report, nil
}

func (v *Validator) walkSiteSecrets() error {
	root := filepath.Join(v.root, "src", "host", "sites")
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat src/host/sites: %w", err)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(filepath.Base(path), ".sops.yml") {
			return nil
		}
		rel := v.rel(path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 5 && parts[0] == "src" && parts[1] == "host" && parts[2] == "sites" && parts[4] == "secrets" && parts[3] != "prod" {
			v.add(rel, "new site bootstrap must not use SOPS secret files; use a local generated seed vars file")
		}
		return nil
	})
}

type ErrorList []ValidationError

func (l ErrorList) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d deployment contract validation errors:", len(l))
	for _, err := range l {
		b.WriteString("\n  - ")
		b.WriteString(err.Error())
	}
	return b.String()
}

func (v *Validator) walkDeployFiles() error {
	for relRoot := range allowedRoot {
		root := filepath.Join(v.root, filepath.FromSlash(relRoot))
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("stat %s: %w", relRoot, err)
		}
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(filepath.Dir(path)) != "deploy" || filepath.Ext(path) != ".yml" {
				return nil
			}
			rel := v.rel(path)
			v.report.DeployFiles++
			v.validateDeployFile(rel, path)
			return nil
		}); err != nil {
			return fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	return nil
}

func (v *Validator) walkIntegrationCatalogs() error {
	root := filepath.Join(v.root, "src", "integrations", "catalog", "sites")
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat src/integrations/catalog/sites: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read src/integrations/catalog/sites: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yml" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		rel := v.rel(path)
		v.report.IntegrationFiles++
		v.validateIntegrationCatalog(rel, path)
	}
	return nil
}

func (v *Validator) walkNomadSpecs() error {
	root := filepath.Join(v.root, "src")
	forbidden := []string{
		"https://verself.sh",
		"verself.sh",
		"guardianintelligence.org",
		"spiffe.verself.sh",
		"inst_5NZSEA08R8P3HN566DNH8D301M",
		"3370540",
		"Iv23liDpxGOmBSQwSJ5i",
		"verself-runner",
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != "nomad.hcl" {
			return nil
		}
		rel := v.rel(path)
		v.report.NomadSpecs++
		body, err := os.ReadFile(path)
		if err != nil {
			v.add(rel, "read: "+err.Error())
			return nil
		}
		text := string(body)
		for _, literal := range forbidden {
			if strings.Contains(text, literal) {
				v.add(rel, fmt.Sprintf("authored Nomad spec contains environment literal %q; use __VERSELF_* site tokens", literal))
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walk src nomad specs: %w", err)
	}
	return nil
}

func (v *Validator) validateDeployFile(rel, path string) {
	if !validOwnerPath(rel) {
		v.add(rel, "deploy declarations must live under src/services, src/infrastructure-components, or src/viteplus-monorepo/apps")
	}
	switch filepath.Base(rel) {
	case "postgres.yml":
		var doc PostgresFile
		if v.decode(rel, path, &doc) {
			v.validatePostgres(rel, doc)
		}
	case "runtime-secrets.yml", "openbao.yml":
		var doc RuntimeSecretsFile
		if v.decode(rel, path, &doc) {
			v.validateRuntimeSecrets(rel, doc)
		}
	case "credstore.yml":
		var doc CredstoreFile
		if v.decode(rel, path, &doc) {
			v.validateCredstore(rel, doc)
		}
	case "public-routes.yml":
		var doc PublicRoutesFile
		if v.decode(rel, path, &doc) {
			v.validatePublicRoutes(rel, doc)
		}
	case "clickhouse-client.yml":
		var doc ClickHouseClientFile
		if v.decode(rel, path, &doc) {
			v.validateClickHouse(rel, doc)
		}
	case "gates.yml":
		var doc GatesFile
		if v.decode(rel, path, &doc) {
			v.validateGates(rel, doc)
		}
	default:
		v.add(rel, "unsupported deploy declaration file name")
	}
}

func (v *Validator) decode(rel, path string, out any) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		v.add(rel, "read: "+err.Error())
		return false
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		v.add(rel, "decode yaml: "+err.Error())
		return false
	}
	return true
}

func (v *Validator) rel(path string) string {
	rel, err := filepath.Rel(v.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func (v *Validator) add(path, msg string) {
	v.errs = append(v.errs, ValidationError{Path: path, Message: msg})
}

func (v *Validator) claim(path string, seen map[string]string, kind, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if prior := seen[key]; prior != "" && prior != path {
		v.add(path, fmt.Sprintf("%s %q is also declared in %s", kind, key, prior))
		return
	}
	seen[key] = path
}

func validOwnerPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 4 {
		return false
	}
	prefix := strings.Join(parts[:2], "/")
	if prefix == "src/viteplus-monorepo" {
		return len(parts) >= 5 && strings.Join(parts[:3], "/") == "src/viteplus-monorepo/apps"
	}
	return allowedRoot[prefix]
}

func requireName(v *Validator, rel, field, value string) {
	if !nameRE.MatchString(strings.TrimSpace(value)) {
		v.add(rel, fmt.Sprintf("%s must match %s", field, nameRE.String()))
	}
}

func requirePathPrefix(v *Validator, rel, field, value, prefix string) {
	if !strings.HasPrefix(strings.TrimSpace(value), prefix) {
		v.add(rel, fmt.Sprintf("%s must start with %s", field, prefix))
	}
}

func exactlyOne(values ...string) bool {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count == 1
}

func sortedJSON(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(body)
}
