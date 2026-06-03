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
	jobIDRE     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	secretRE    = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_]+)+$`)
	durationRE  = regexp.MustCompile(`^[1-9][0-9]*(ms|s|m|h)$`)
	siteNameRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	apiKeyRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	allowedRoot = map[string]bool{
		"src/infrastructure-components": true,
		"src/integrations":              true,
		"src/services":                  true,
		"src/viteplus-monorepo/apps":    true,
	}
)

type Report struct {
	DeployFiles      int `json:"deploy_files"`
	IntegrationFiles int `json:"integration_files"`
	PostgresFiles    int `json:"postgres_files"`
	RuntimeSecrets   int `json:"runtime_secrets"`
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
	root                    string
	report                  Report
	errs                    []ValidationError
	seen                    seenClaims
	generatedRuntimeSecrets bool
}

type seenClaims struct {
	postgresDatabases map[string]string
	postgresOwners    map[string]string
	replicationRoles  map[string]string
	publicRouteHosts  map[string]string
	publicAPIKeys     map[string]string
	runtimeSecrets    map[string]string
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

type githubWorkflowFile struct {
	Permissions map[string]string        `yaml:"permissions"`
	Jobs        map[string]githubJobFile `yaml:"jobs"`
}

type githubJobFile struct {
	If          string                 `yaml:"if"`
	Permissions map[string]string      `yaml:"permissions"`
	Steps       []githubWorkflowStep   `yaml:"steps"`
	Raw         map[string]interface{} `yaml:",inline"`
}

type githubWorkflowStep struct {
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
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
	if err := v.walkSiteVars(); err != nil {
		return Report{}, err
	}
	v.validateNoToolLayerDeployEngine()
	v.validateBootstrapRuntimeSecretContracts()
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

func (v *Validator) walkSiteVars() error {
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
		if d.IsDir() || filepath.Base(path) != "vars.yml" {
			return nil
		}
		rel := v.rel(path)
		values := map[string]any{}
		if v.decode(rel, path, &values) {
			v.validateDeploymentWorkflowRefs(rel, values)
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

func (v *Validator) validateDeploymentWorkflowRefs(rel string, values map[string]any) {
	repositories := csvSet(siteVarsString(values, "deployment_github_allowed_repositories"))
	refs := csvSet(siteVarsString(values, "deployment_github_allowed_refs"))
	raw := siteVarsString(values, "deployment_github_allowed_workflow_refs")
	if raw == "" {
		if len(repositories) > 0 || len(refs) > 0 {
			v.add(rel, "GitHub OIDC deployment auth requires deployment_github_allowed_repositories, deployment_github_allowed_refs, and deployment_github_allowed_workflow_refs together")
		}
		return
	}
	if len(repositories) == 0 || len(refs) == 0 {
		v.add(rel, "GitHub OIDC deployment auth requires deployment_github_allowed_repositories, deployment_github_allowed_refs, and deployment_github_allowed_workflow_refs together")
	}
	for _, ref := range strings.Split(raw, ",") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		workflow, gitRef, ok := strings.Cut(ref, "@")
		if !ok || gitRef == "" || !strings.Contains(workflow, "/.github/workflows/") {
			v.add(rel, fmt.Sprintf("deployment_github_allowed_workflow_refs entry %q must be a GitHub workflow ref", ref))
			continue
		}
		repo, workflowFile, ok := strings.Cut(workflow, "/.github/workflows/")
		workflowFileInvalid := workflowFile == "" ||
			strings.Contains(workflowFile, "/") ||
			(!strings.HasSuffix(workflowFile, ".yml") && !strings.HasSuffix(workflowFile, ".yaml"))
		if !ok || repo == "" || workflowFileInvalid {
			v.add(rel, fmt.Sprintf("deployment_github_allowed_workflow_refs entry %q must point at a workflow file directly under .github/workflows", ref))
			continue
		}
		if len(repositories) > 0 {
			if _, ok := repositories[repo]; !ok {
				v.add(rel, fmt.Sprintf("deployment_github_allowed_workflow_refs entry %q repository is not listed in deployment_github_allowed_repositories", ref))
			}
		}
		if len(refs) > 0 {
			if _, ok := refs[gitRef]; !ok {
				v.add(rel, fmt.Sprintf("deployment_github_allowed_workflow_refs entry %q ref is not listed in deployment_github_allowed_refs", ref))
			}
		}
		if repo != "guardian-intelligence/verself" {
			continue
		}
		repoPath := filepath.Join(".github", "workflows", workflowFile)
		absPath := filepath.Join(v.root, repoPath)
		if _, err := os.Stat(absPath); err != nil {
			v.add(rel, fmt.Sprintf("deployment_github_allowed_workflow_refs entry %q points at a missing first-party workflow file", ref))
			continue
		}
		v.validateDeploymentWorkflowFile(filepath.ToSlash(repoPath), absPath, repo, gitRef, siteNameFromVarsRel(rel))
	}
}

func (v *Validator) validateDeploymentWorkflowFile(rel, path, repo, gitRef, site string) {
	body, err := os.ReadFile(path)
	if err != nil {
		v.add(rel, "read: "+err.Error())
		return
	}
	text := string(body)
	for _, forbidden := range []string{"pull_request:", "pull_request_target:"} {
		if strings.Contains(text, forbidden) {
			v.add(rel, "deployment workflows must not run from pull request events")
		}
	}
	var workflow githubWorkflowFile
	dec := yaml.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&workflow); err != nil {
		v.add(rel, "decode workflow yaml: "+err.Error())
		return
	}
	v.validateWorkflowPermissions(rel, "permissions", workflow.Permissions)
	if len(workflow.Jobs) == 0 {
		v.add(rel, "deployment workflow must declare at least one job")
		return
	}
	deployJobs := 0
	for name, job := range workflow.Jobs {
		if len(job.Permissions) > 0 {
			v.validateWorkflowPermissions(rel, "jobs."+name+".permissions", job.Permissions)
		}
		if !strings.Contains(job.If, "github.repository == '"+repo+"'") || !strings.Contains(job.If, "github.ref == '"+gitRef+"'") {
			v.add(rel, fmt.Sprintf("deployment job %q must guard github.repository=%q and github.ref=%q", name, repo, gitRef))
		}
		for i, step := range job.Steps {
			if strings.TrimSpace(step.Uses) != "" {
				v.validateWorkflowActionUse(rel, name, i, step.Uses)
			}
			if deploymentRunMatches(step.Run, site) {
				deployJobs++
			}
		}
	}
	if deployJobs == 0 {
		v.add(rel, fmt.Sprintf("deployment workflow must run aspect deploy --site=%s --sha=$GITHUB_SHA", site))
	}
}

func (v *Validator) validateWorkflowPermissions(rel, field string, permissions map[string]string) {
	if len(permissions) == 0 {
		v.add(rel, field+" must explicitly grant contents: read and id-token: write")
		return
	}
	if strings.ToLower(strings.TrimSpace(permissions["contents"])) != "read" {
		v.add(rel, field+" must grant contents: read")
	}
	if strings.ToLower(strings.TrimSpace(permissions["id-token"])) != "write" {
		v.add(rel, field+" must grant id-token: write")
	}
	for permission, value := range permissions {
		permission = strings.TrimSpace(permission)
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "write" && permission != "id-token" {
			v.add(rel, fmt.Sprintf("%s must not grant %s: write", field, permission))
		}
	}
}

func (v *Validator) validateWorkflowActionUse(rel, job string, step int, action string) {
	action = strings.TrimSpace(action)
	if strings.HasPrefix(action, "./.github/actions/") {
		return
	}
	if strings.HasPrefix(action, "guardian-intelligence/verself/.github/actions/") {
		return
	}
	v.add(rel, fmt.Sprintf("deployment job %q step %d must not use external action %q", job, step+1, action))
}

func deploymentRunMatches(run, site string) bool {
	run = strings.TrimSpace(run)
	if site == "" || !strings.Contains(run, "aspect deploy") {
		return false
	}
	if !strings.Contains(run, "--site="+site) {
		return false
	}
	return strings.Contains(run, `--sha="$GITHUB_SHA"`) || strings.Contains(run, "--sha=$GITHUB_SHA")
}

func siteNameFromVarsRel(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 4 && parts[0] == "src" && parts[1] == "host" && parts[2] == "sites" {
		return parts[3]
	}
	return ""
}

func siteVarsString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func csvSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func (v *Validator) validateBootstrapRuntimeSecretContracts() {
	r2SecretsPath := filepath.Join(v.root, "src", "integrations", "cloudflare", "r2-control-plane", "deploy", "runtime-secrets.yml")
	if _, err := os.Stat(r2SecretsPath); err == nil {
		rel := v.rel(r2SecretsPath)
		var doc RuntimeSecretsFile
		if v.decode(rel, r2SecretsPath, &doc) {
			v.requireExternalOpenBaoSecret(rel, doc, "cloudflare-r2-control-plane.publisher_token_id")
			v.requireExternalOpenBaoSecret(rel, doc, "cloudflare-r2-control-plane.publisher_secret_access_key")
		}
	}
	emailSecretsPath := filepath.Join(v.root, "src", "services", "email-service", "deploy", "runtime-secrets.yml")
	if _, err := os.Stat(emailSecretsPath); err == nil {
		rel := v.rel(emailSecretsPath)
		var doc RuntimeSecretsFile
		if v.decode(rel, emailSecretsPath, &doc) {
			v.requireExternalOpenBaoSecret(rel, doc, "email-service.resend.full_access_api_key")
			v.requireProducedSecret(rel, doc, "email-service.resend.api_key", "email-service-resend-keys")
			v.requireProducedSecret(rel, doc, "email-service.resend.key_metadata", "email-service-resend-keys")
			v.requireProducedSecret(rel, doc, "zitadel.smtp.password", "email-service-resend-keys")
			if declaration, ok := runtimeDeclarationByName(doc, "zitadel.smtp.password"); ok && !stringSliceContains(declaration.ConsumerJobIDs, "zitadel") {
				v.add(rel, "bootstrap runtime secret zitadel.smtp.password must declare consumer_job_ids: [zitadel]")
			}
		}
		v.requireEmailServiceResendKeyManagerBoundary("src/services/email-service/resend-keys.nomad.hcl")
	}
	githubSecretsPath := filepath.Join(v.root, "src", "services", "github-integration-service", "deploy", "runtime-secrets.yml")
	if _, err := os.Stat(githubSecretsPath); err == nil {
		rel := v.rel(githubSecretsPath)
		var doc RuntimeSecretsFile
		if v.decode(rel, githubSecretsPath, &doc) {
			v.requireExternalOpenBaoSecret(rel, doc, "github-integration-service.github.private_key")
			v.requireExternalOpenBaoSecret(rel, doc, "github-integration-service.github.webhook_secret")
			v.requireExternalOpenBaoSecret(rel, doc, "github-integration-service.github.oauth_client_secret")
		}
	}
	zitadelSecretsPath := filepath.Join(v.root, "src", "infrastructure-components", "zitadel", "deploy", "runtime-secrets.yml")
	if _, err := os.Stat(zitadelSecretsPath); err == nil {
		rel := v.rel(zitadelSecretsPath)
		var doc RuntimeSecretsFile
		if v.decode(rel, zitadelSecretsPath, &doc) {
			v.requireExternalOpenBaoSecret(rel, doc, "auth-control-plane.github_login.oauth_client_secret")
		}
	}
	v.requireNomadRuntimeSecretReferences("src/integrations/cloudflare/r2-control-plane/nomad.hcl", []string{
		"cloudflare-r2-control-plane.publisher_token_id",
		"cloudflare-r2-control-plane.publisher_secret_access_key",
	})
	v.requireR2ControlPlaneServeBoundary("src/integrations/cloudflare/r2-control-plane/nomad.hcl")
}

func (v *Validator) validateNoToolLayerDeployEngine() {
	for _, rel := range []string{
		"src/tools/deployment/deployengine",
		"src/tools/deployment/internal/bazelbuild",
		"src/tools/deployment/internal/bep",
		"src/tools/deployment/internal/deploycontract",
		"src/tools/deployment/internal/deploymodel",
		"src/tools/deployment/internal/nomadclient",
		"src/tools/deployment/internal/runtime",
		"src/tools/deployment/internal/siteconfig",
		"src/tools/deployment/internal/siteinject",
		"src/tools/deployment/internal/sshtun",
	} {
		path := filepath.Join(v.root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			v.add(rel, "normal deployment runtime code must live under src/services/deployment-service; src/tools/deployment is limited to thin clients and first-bootstrap tooling")
		} else if err != nil && !os.IsNotExist(err) {
			v.add(rel, "inspect: "+err.Error())
		}
	}
}

func (v *Validator) requireProducedSecret(rel string, doc RuntimeSecretsFile, name string, producedByJob string) {
	declaration, ok := runtimeDeclarationByName(doc, name)
	if !ok {
		v.add(rel, fmt.Sprintf("bootstrap runtime secret %s is required", name))
		return
	}
	if strings.TrimSpace(declaration.ProducedByJob) != producedByJob || declaration.Generated.Bytes != 0 || declaration.ExternalOpenBao {
		v.add(rel, fmt.Sprintf("bootstrap runtime secret %s must be produced_by_job: %s", name, producedByJob))
	}
}

func (v *Validator) requireExternalOpenBaoSecret(rel string, doc RuntimeSecretsFile, name string) {
	declaration, ok := runtimeDeclarationByName(doc, name)
	if !ok {
		v.add(rel, fmt.Sprintf("bootstrap runtime secret %s is required", name))
		return
	}
	if !declaration.ExternalOpenBao || strings.TrimSpace(declaration.ProducedByJob) != "" || declaration.Generated.Bytes != 0 {
		v.add(rel, fmt.Sprintf("bootstrap runtime secret %s must be external_openbao", name))
	}
}

func (v *Validator) requireNomadRuntimeSecretReferences(rel string, secrets []string) {
	path := filepath.Join(v.root, filepath.FromSlash(rel))
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		v.add(rel, "read: "+err.Error())
		return
	}
	text := string(body)
	for _, secret := range secrets {
		if !strings.Contains(text, "kv-runtime/data/secret/org/"+secret) {
			v.add(rel, fmt.Sprintf("bootstrap Nomad job must render OpenBao runtime secret %s", secret))
		}
	}
}

func (v *Validator) requireR2ControlPlaneServeBoundary(rel string) {
	path := filepath.Join(v.root, filepath.FromSlash(rel))
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		v.add(rel, "read: "+err.Error())
		return
	}
	text := string(body)
	for _, required := range []string{
		`"--action=serve"`,
		`"--credential-source=files"`,
		`"--parent-access-key-id-file=$${NOMAD_SECRETS_DIR}/r2-publisher-token-id"`,
		`"--parent-secret-access-key-file=$${NOMAD_SECRETS_DIR}/r2-publisher-secret-access-key"`,
		`"--listen=127.0.0.1:$${NOMAD_PORT_internal_https}"`,
		`SPIFFE_ENDPOINT_SOCKET = "unix:///run/spire-agent/sockets/agent.sock"`,
		`name = "cloudflare-r2-control-plane-internal-https"`,
	} {
		if !strings.Contains(text, required) {
			v.add(rel, fmt.Sprintf("R2 control-plane runtime must declare %s", required))
		}
	}
}

func (v *Validator) requireEmailServiceResendKeyManagerBoundary(rel string) {
	path := filepath.Join(v.root, filepath.FromSlash(rel))
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		v.add(rel, "email-service must own a Resend child-key Nomad job")
		return
	}
	if err != nil {
		v.add(rel, "read: "+err.Error())
		return
	}
	text := string(body)
	for _, required := range []string{
		`job "email-service-resend-keys"`,
		`role = "email-service-resend-keys-runtime"`,
		`env  = false`,
		`"resend-keys"`,
		`"apply"`,
		`"--openbao-token-file=$${NOMAD_SECRETS_DIR}/vault_token"`,
		`VERSELF_SITE = "__VERSELF_SITE__"`,
	} {
		if !strings.Contains(text, required) {
			v.add(rel, fmt.Sprintf("email-service Resend key manager must declare %s", required))
		}
	}
}

func runtimeDeclarationByName(doc RuntimeSecretsFile, name string) (RuntimeSecretDeclaration, bool) {
	for _, declaration := range doc.Declarations {
		if strings.TrimSpace(declaration.Name) == name {
			return declaration, true
		}
	}
	return RuntimeSecretDeclaration{}, false
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (v *Validator) walkNomadSpecs() error {
	root := filepath.Join(v.root, "src")
	forbidden := []string{
		"/etc/credstore",
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
		v.add(rel, "deploy declarations must live under src/services, src/integrations, src/infrastructure-components, or src/viteplus-monorepo/apps")
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

func exactlyOneRuntimeSecretSource(producedByJob string, generatedBytes int, externalOpenBao bool) bool {
	count := 0
	if strings.TrimSpace(producedByJob) != "" {
		count++
	}
	if generatedBytes != 0 {
		count++
	}
	if externalOpenBao {
		count++
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
