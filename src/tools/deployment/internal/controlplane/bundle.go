package controlplane

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/verself/deployment-tools/internal/deploycontract"
)

const (
	BundleSchemaVersion = "verself.substrate-control-plane.v1"
	RuntimeMount        = "kv-runtime"
	NomadJWTMount       = "jwt-nomad"
	NomadAudience       = "vault.io"
	ControlPlaneJobID   = "substrate-control-plane"
)

var (
	nameRE       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	jobIDRE      = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	secretNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_]+)+$`)
)

type Component struct {
	Component string
	JobID     string
	JobSpec   string
}

type Bundle struct {
	SchemaVersion string         `json:"schema_version"`
	Site          string         `json:"site"`
	SHA           string         `json:"sha"`
	DeployRunKey  string         `json:"deploy_run_key"`
	OpenBao       OpenBaoBundle  `json:"openbao"`
	Postgres      PostgresBundle `json:"postgres"`
}

type OpenBaoBundle struct {
	RuntimeSecrets []RuntimeSecret `json:"runtime_secrets"`
	NomadRoles     []NomadRole     `json:"nomad_roles"`
}

type RuntimeSecret struct {
	Name           string              `json:"name"`
	OwnerPath      string              `json:"owner_path"`
	Component      string              `json:"component"`
	JobID          string              `json:"job_id"`
	ConsumerJobIDs []string            `json:"consumer_job_ids,omitempty"`
	Source         RuntimeSecretSource `json:"source"`
}

type RuntimeSecretSource struct {
	Kind           string `json:"kind"`
	SiteSecretKey  string `json:"site_secret_key,omitempty"`
	ProducedByJob  string `json:"produced_by_job,omitempty"`
	GeneratedBytes int    `json:"generated_bytes,omitempty"`
	Encoding       string `json:"encoding,omitempty"`
}

const (
	RuntimeSecretSourceSiteSecret = "site_secret"
	RuntimeSecretSourceGenerated  = "generated"
	RuntimeSecretSourceProduced   = "produced_by_job"
)

type NomadRole struct {
	Name         string   `json:"name"`
	Policy       string   `json:"policy"`
	Component    string   `json:"component"`
	JobID        string   `json:"job_id"`
	Secrets      []string `json:"secrets"`
	WriteSecrets []string `json:"write_secrets,omitempty"`
}

type PostgresBundle struct {
	Databases        []PostgresDatabase        `json:"databases"`
	ReplicationRoles []PostgresReplicationRole `json:"replication_roles"`
	PeerMappings     []PostgresPeerMapping     `json:"peer_mappings"`
	Publications     []PostgresPublication     `json:"publications"`
}

type PostgresDatabase struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Path  string `json:"path"`
}

type PostgresReplicationRole struct {
	Name            string `json:"name"`
	ConnectionLimit int    `json:"connection_limit"`
	OpenBaoSecret   string `json:"openbao_secret"`
	Path            string `json:"path"`
}

type PostgresPeerMapping struct {
	SystemUser string `json:"system_user"`
	PGUser     string `json:"pg_user"`
	Path       string `json:"path"`
}

type PostgresPublication struct {
	Database         string   `json:"database"`
	Publication      string   `json:"publication"`
	PublicationOwner string   `json:"publication_owner"`
	TableOwner       string   `json:"table_owner"`
	ReplicationRole  string   `json:"replication_role"`
	Tables           []string `json:"tables"`
	Path             string   `json:"path"`
}

func LoadBundle(repoRoot, site, sha, deployRunKey string, components []Component) (Bundle, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return Bundle{}, fmt.Errorf("repo root is required")
	}
	secrets, err := loadRuntimeSecrets(repoRoot, site, components)
	if err != nil {
		return Bundle{}, err
	}
	postgres, err := loadPostgres(repoRoot)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		SchemaVersion: BundleSchemaVersion,
		Site:          strings.TrimSpace(site),
		SHA:           strings.TrimSpace(sha),
		DeployRunKey:  strings.TrimSpace(deployRunKey),
		OpenBao: OpenBaoBundle{
			RuntimeSecrets: secrets,
			NomadRoles:     buildNomadRoles(secrets),
		},
		Postgres: postgres,
	}, nil
}

func loadRuntimeSecrets(repoRoot, _ string, components []Component) ([]RuntimeSecret, error) {
	owners, err := componentOwners(repoRoot, components)
	if err != nil {
		return nil, err
	}
	componentByJob := map[string]Component{}
	for _, component := range components {
		componentByJob[component.JobID] = component
	}
	ownerByRoot := map[string][]Component{}
	for _, owner := range owners {
		ownerByRoot[owner.ownerRoot] = append(ownerByRoot[owner.ownerRoot], owner.component)
	}
	out := []RuntimeSecret{}
	err = walkDeployFiles(repoRoot, func(path, rel string) error {
		name := filepath.Base(path)
		if name != "runtime-secrets.yml" && name != "openbao.yml" {
			return nil
		}
		ownerRoot := strings.TrimSuffix(filepath.ToSlash(filepath.Dir(path)), "/deploy")
		ownerRel, err := filepath.Rel(repoRoot, ownerRoot)
		if err != nil {
			return err
		}
		ownerRel = filepath.ToSlash(ownerRel)
		components, ok := ownerByRoot[ownerRel]
		if !ok {
			return fmt.Errorf("%s: runtime secret owner has no Nomad component", rel)
		}
		var doc deploycontract.RuntimeSecretsFile
		if err := decodeYAML(path, &doc); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		for _, seed := range doc.Seeds {
			component, err := resolveSeedComponent(rel, seed, components)
			if err != nil {
				return err
			}
			secret := RuntimeSecret{
				Name:           strings.TrimSpace(seed.Name),
				OwnerPath:      rel,
				Component:      component.Component,
				JobID:          component.JobID,
				ConsumerJobIDs: normalizedConsumerJobIDs(seed.ConsumerJobIDs),
				Source:         runtimeSecretSource(seed),
			}
			for _, consumerJobID := range secret.ConsumerJobIDs {
				if _, ok := componentByJob[consumerJobID]; !ok {
					return fmt.Errorf("%s: consumer_job_ids entry %q does not match a Nomad component", rel, consumerJobID)
				}
			}
			if secret.Source.Kind == RuntimeSecretSourceProduced {
				if _, ok := componentByJob[secret.Source.ProducedByJob]; !ok {
					return fmt.Errorf("%s: produced_by_job %q does not match a Nomad component", rel, secret.Source.ProducedByJob)
				}
			}
			if err := validateRuntimeSecret(secret); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			out = append(out, secret)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].JobID != out[j].JobID {
			return out[i].JobID < out[j].JobID
		}
		return out[i].Name < out[j].Name
	})
	seen := map[string]string{}
	for _, secret := range out {
		if prior := seen[secret.Name]; prior != "" && prior != secret.OwnerPath {
			return nil, fmt.Errorf("%s: runtime secret %q is also declared in %s", secret.OwnerPath, secret.Name, prior)
		}
		seen[secret.Name] = secret.OwnerPath
	}
	return out, nil
}

func runtimeSecretSource(seed deploycontract.RuntimeSecretSeed) RuntimeSecretSource {
	if key := strings.TrimSpace(seed.SiteSecret); key != "" {
		return RuntimeSecretSource{Kind: RuntimeSecretSourceSiteSecret, SiteSecretKey: key}
	}
	if jobID := strings.TrimSpace(seed.ProducedByJob); jobID != "" {
		return RuntimeSecretSource{Kind: RuntimeSecretSourceProduced, ProducedByJob: jobID}
	}
	if seed.Generated.Bytes != 0 {
		return RuntimeSecretSource{
			Kind:           RuntimeSecretSourceGenerated,
			GeneratedBytes: seed.Generated.Bytes,
			Encoding:       strings.TrimSpace(seed.Generated.Encoding),
		}
	}
	return RuntimeSecretSource{}
}

type componentOwner struct {
	component Component
	ownerRoot string
}

func componentOwners(repoRoot string, components []Component) ([]componentOwner, error) {
	owners := make([]componentOwner, 0, len(components))
	for _, component := range components {
		jobSpec := strings.TrimSpace(component.JobSpec)
		if jobSpec == "" {
			return nil, fmt.Errorf("%s: component job spec is required", component.JobID)
		}
		rel, err := filepath.Rel(repoRoot, resolvePath(repoRoot, jobSpec))
		if err != nil {
			return nil, fmt.Errorf("%s: resolve job spec owner: %w", component.JobID, err)
		}
		rel = filepath.ToSlash(rel)
		ownerRoot := strings.TrimSuffix(rel, "/nomad.hcl")
		if ownerRoot == rel {
			ownerRoot = filepath.ToSlash(filepath.Dir(rel))
		}
		owners = append(owners, componentOwner{component: component, ownerRoot: ownerRoot})
	}
	return owners, nil
}

func resolveSeedComponent(rel string, seed deploycontract.RuntimeSecretSeed, components []Component) (Component, error) {
	jobID := strings.TrimSpace(seed.JobID)
	if jobID == "" {
		if len(components) != 1 {
			return Component{}, fmt.Errorf("%s: runtime secret owner must map to exactly one Nomad job, got %d", rel, len(components))
		}
		return components[0], nil
	}
	for _, component := range components {
		if component.JobID == jobID {
			return component, nil
		}
	}
	return Component{}, fmt.Errorf("%s: runtime secret job_id %q does not match an owner Nomad job", rel, jobID)
}

func validateRuntimeSecret(secret RuntimeSecret) error {
	if !secretNameRE.MatchString(secret.Name) {
		return fmt.Errorf("runtime secret %q must match %s", secret.Name, secretNameRE.String())
	}
	seenConsumers := map[string]bool{}
	for _, consumerJobID := range secret.ConsumerJobIDs {
		if !jobIDRE.MatchString(consumerJobID) {
			return fmt.Errorf("%s consumer job %q must match %s", secret.Name, consumerJobID, jobIDRE.String())
		}
		if consumerJobID == secret.JobID {
			return fmt.Errorf("%s consumer job %q duplicates owner job", secret.Name, consumerJobID)
		}
		if seenConsumers[consumerJobID] {
			return fmt.Errorf("%s consumer job %q is duplicated", secret.Name, consumerJobID)
		}
		seenConsumers[consumerJobID] = true
	}
	sources := 0
	switch secret.Source.Kind {
	case RuntimeSecretSourceSiteSecret:
		sources++
		if !nameRE.MatchString(secret.Source.SiteSecretKey) {
			return fmt.Errorf("%s site_secret_key must match %s", secret.Name, nameRE.String())
		}
	case RuntimeSecretSourceGenerated:
		sources++
		if secret.Source.GeneratedBytes < 16 || secret.Source.GeneratedBytes > 96 {
			return fmt.Errorf("%s generated.bytes must be between 16 and 96", secret.Name)
		}
		switch secret.Source.Encoding {
		case "", "base64url", "hex", "alphanumeric":
		default:
			return fmt.Errorf("%s generated.encoding must be base64url, hex, or alphanumeric", secret.Name)
		}
	case RuntimeSecretSourceProduced:
		sources++
		if !jobIDRE.MatchString(secret.Source.ProducedByJob) {
			return fmt.Errorf("%s produced_by_job must match %s", secret.Name, jobIDRE.String())
		}
	case "":
	default:
		return fmt.Errorf("%s source.kind must be site_secret, generated, or produced_by_job", secret.Name)
	}
	if sources != 1 {
		return fmt.Errorf("%s must declare exactly one source: site_secret, generated, or produced_by_job", secret.Name)
	}
	return nil
}

func buildNomadRoles(secrets []RuntimeSecret) []NomadRole {
	byJob := map[string][]RuntimeSecret{}
	writesByJob := map[string][]string{}
	for _, secret := range secrets {
		byJob[secret.JobID] = append(byJob[secret.JobID], secret)
		for _, consumerJobID := range secret.ConsumerJobIDs {
			byJob[consumerJobID] = append(byJob[consumerJobID], secret)
		}
		if secret.Source.Kind == RuntimeSecretSourceProduced {
			writesByJob[secret.Source.ProducedByJob] = append(writesByJob[secret.Source.ProducedByJob], secret.Name)
		}
	}
	jobsSeen := map[string]bool{}
	for job := range byJob {
		jobsSeen[job] = true
	}
	for job := range writesByJob {
		jobsSeen[job] = true
	}
	jobs := make([]string, 0, len(jobsSeen))
	for job := range jobsSeen {
		jobs = append(jobs, job)
	}
	sort.Strings(jobs)
	roles := make([]NomadRole, 0, len(jobs))
	for _, job := range jobs {
		jobSecrets := byJob[job]
		sort.Slice(jobSecrets, func(i, j int) bool { return jobSecrets[i].Name < jobSecrets[j].Name })
		names := make([]string, 0, len(jobSecrets))
		component := job
		for _, secret := range jobSecrets {
			names = append(names, secret.Name)
			if secret.Component != "" {
				component = secret.Component
			}
		}
		writeSecrets := append([]string(nil), writesByJob[job]...)
		sort.Strings(writeSecrets)
		if component == job && len(jobSecrets) == 0 {
			component = job
		}
		roles = append(roles, NomadRole{
			Name:         job + "-runtime",
			Policy:       job + "-runtime",
			Component:    component,
			JobID:        job,
			Secrets:      names,
			WriteSecrets: writeSecrets,
		})
	}
	return roles
}

func normalizedConsumerJobIDs(raw []string) []string {
	out := []string{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func loadPostgres(repoRoot string) (PostgresBundle, error) {
	postgres := PostgresBundle{
		PeerMappings: []PostgresPeerMapping{{SystemUser: "postgres", PGUser: "postgres", Path: "postgresql:base"}},
	}
	err := walkDeployFiles(repoRoot, func(path, rel string) error {
		if filepath.Base(path) != "postgres.yml" {
			return nil
		}
		var doc deploycontract.PostgresFile
		if err := decodeYAML(path, &doc); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		for _, db := range doc.ServiceDatabases {
			postgres.Databases = append(postgres.Databases, PostgresDatabase{Name: db.Name, Owner: db.Owner, Path: rel})
		}
		for _, role := range doc.ReplicationRoles {
			postgres.ReplicationRoles = append(postgres.ReplicationRoles, PostgresReplicationRole{
				Name:            role.Name,
				ConnectionLimit: role.ConnectionLimit,
				OpenBaoSecret:   role.OpenBaoSecret,
				Path:            rel,
			})
		}
		for _, mapping := range doc.PeerMappings {
			postgres.PeerMappings = append(postgres.PeerMappings, PostgresPeerMapping{SystemUser: mapping.SystemUser, PGUser: mapping.PGUser, Path: rel})
		}
		for _, pub := range doc.LogicalPublications {
			tables := append([]string(nil), pub.Tables...)
			sort.Strings(tables)
			postgres.Publications = append(postgres.Publications, PostgresPublication{
				Database:         pub.Database,
				Publication:      pub.Publication,
				PublicationOwner: pub.PublicationOwner,
				TableOwner:       pub.TableOwner,
				ReplicationRole:  pub.ReplicationRole,
				Tables:           tables,
				Path:             rel,
			})
		}
		return nil
	})
	if err != nil {
		return PostgresBundle{}, err
	}
	sort.Slice(postgres.Databases, func(i, j int) bool { return postgres.Databases[i].Name < postgres.Databases[j].Name })
	sort.Slice(postgres.ReplicationRoles, func(i, j int) bool { return postgres.ReplicationRoles[i].Name < postgres.ReplicationRoles[j].Name })
	sort.Slice(postgres.PeerMappings, func(i, j int) bool {
		if postgres.PeerMappings[i].SystemUser != postgres.PeerMappings[j].SystemUser {
			return postgres.PeerMappings[i].SystemUser < postgres.PeerMappings[j].SystemUser
		}
		return postgres.PeerMappings[i].PGUser < postgres.PeerMappings[j].PGUser
	})
	sort.Slice(postgres.Publications, func(i, j int) bool {
		if postgres.Publications[i].Database != postgres.Publications[j].Database {
			return postgres.Publications[i].Database < postgres.Publications[j].Database
		}
		return postgres.Publications[i].Publication < postgres.Publications[j].Publication
	})
	return postgres, validatePostgres(postgres)
}

func walkDeployFiles(repoRoot string, visit func(path, rel string) error) error {
	for _, relRoot := range []string{"src/infrastructure-components", "src/services", "src/viteplus-monorepo/apps"} {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %s: %w", relRoot, err)
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(filepath.Dir(path)) != "deploy" || filepath.Ext(path) != ".yml" {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			return visit(path, filepath.ToSlash(rel))
		})
		if err != nil {
			return fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	return nil
}

func decodeYAML(path string, out any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode yaml: %w", err)
	}
	return nil
}

func resolvePath(repoRoot, path string) string {
	if filepath.IsAbs(path) || repoRoot == "" {
		return path
	}
	return filepath.Join(repoRoot, filepath.FromSlash(path))
}
