package openbaoruntime

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
)

const (
	RuntimeMount     = "kv-runtime"
	RuntimeNamespace = "runtime"
	NomadJWTMount    = "jwt-nomad"
	SpiffeJWTMount   = "spiffe-jwt"
	NomadAudience    = "vault.io"
	SpiffeAudience   = "openbao"
)

var secretNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_]+)+$`)

type Component struct {
	Component string
	JobID     string
	JobSpec   string
}

type Plan struct {
	Site       string
	Secrets    []Secret
	NomadRoles []NomadRole
}

type Secret struct {
	Name       string
	OwnerPath  string
	Component  string
	JobID      string
	SiteSecret string
	File       string
	Generated  GeneratedSecret
}

type NomadRole struct {
	Name      string
	Policy    string
	Component string
	JobID     string
	Secrets   []string
}

type runtimeSecretsFile struct {
	Seeds []runtimeSecretSeed `yaml:"openbao_runtime_secret_seed_declarations"`
}

type runtimeSecretSeed struct {
	Name       string `yaml:"name"`
	JobID      string `yaml:"job_id"`
	SiteSecret string `yaml:"site_secret"`
	File       string `yaml:"file"`
	Generated  struct {
		Bytes int `yaml:"bytes"`
	} `yaml:"generated"`
}

type componentOwner struct {
	component Component
	ownerRoot string
}

type GeneratedSecret struct {
	Bytes int
}

func LoadPlan(repoRoot, site string, components []Component) (Plan, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return Plan{}, fmt.Errorf("repo root is required")
	}
	owners, err := componentOwners(repoRoot, components)
	if err != nil {
		return Plan{}, err
	}
	secrets, err := loadSecrets(repoRoot, owners)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Site:       strings.TrimSpace(site),
		Secrets:    secrets,
		NomadRoles: buildNomadRoles(secrets),
	}, nil
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

func loadSecrets(repoRoot string, owners []componentOwner) ([]Secret, error) {
	ownerByRoot := map[string][]Component{}
	for _, owner := range owners {
		ownerByRoot[owner.ownerRoot] = append(ownerByRoot[owner.ownerRoot], owner.component)
	}
	var out []Secret
	for _, relRoot := range []string{"src/infrastructure-components", "src/services", "src/viteplus-monorepo/apps"} {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", relRoot, err)
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(filepath.Dir(path)) != "deploy" {
				return nil
			}
			name := filepath.Base(path)
			if name != "runtime-secrets.yml" && name != "openbao.yml" {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
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
			seeds, err := readRuntimeSecretsFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			for _, seed := range seeds {
				component, err := resolveSeedComponent(rel, seed, components)
				if err != nil {
					return err
				}
				secret := Secret{
					Name:       strings.TrimSpace(seed.Name),
					OwnerPath:  rel,
					Component:  component.Component,
					JobID:      component.JobID,
					SiteSecret: strings.TrimSpace(seed.SiteSecret),
					File:       strings.TrimSpace(seed.File),
					Generated:  GeneratedSecret{Bytes: seed.Generated.Bytes},
				}
				if err := validateSecret(secret); err != nil {
					return fmt.Errorf("%s: %w", rel, err)
				}
				out = append(out, secret)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", relRoot, err)
		}
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

func resolveSeedComponent(rel string, seed runtimeSecretSeed, components []Component) (Component, error) {
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

func readRuntimeSecretsFile(path string) ([]runtimeSecretSeed, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var doc runtimeSecretsFile
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode yaml: %w", err)
	}
	return doc.Seeds, nil
}

func validateSecret(secret Secret) error {
	if !secretNameRE.MatchString(secret.Name) {
		return fmt.Errorf("runtime secret %q must match %s", secret.Name, secretNameRE.String())
	}
	sources := 0
	if secret.SiteSecret != "" {
		sources++
	}
	if secret.File != "" {
		sources++
	}
	if secret.Generated.Bytes != 0 {
		sources++
	}
	if sources != 1 {
		return fmt.Errorf("%s must declare exactly one source: site_secret, file, or generated", secret.Name)
	}
	if secret.File != "" && !filepath.IsAbs(secret.File) {
		return fmt.Errorf("%s file source must be absolute", secret.Name)
	}
	if secret.Generated.Bytes != 0 && (secret.Generated.Bytes < 16 || secret.Generated.Bytes > 96) {
		return fmt.Errorf("%s generated.bytes must be between 16 and 96", secret.Name)
	}
	return nil
}

func buildNomadRoles(secrets []Secret) []NomadRole {
	byJob := map[string]NomadRole{}
	for _, secret := range secrets {
		role := byJob[secret.JobID]
		if role.JobID == "" {
			role = NomadRole{
				Name:      sanitizeName(secret.JobID + "-runtime"),
				Policy:    sanitizeName("nomad-runtime-" + secret.JobID),
				Component: secret.Component,
				JobID:     secret.JobID,
			}
		}
		role.Secrets = append(role.Secrets, secret.Name)
		byJob[secret.JobID] = role
	}
	jobIDs := make([]string, 0, len(byJob))
	for jobID := range byJob {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)
	roles := make([]NomadRole, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		role := byJob[jobID]
		sort.Strings(role.Secrets)
		role.Secrets = dedupeStrings(role.Secrets)
		roles = append(roles, role)
	}
	return roles
}

func resolvePath(repoRoot, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(repoRoot, filepath.FromSlash(path))
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "runtime"
	}
	return out
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	var prev string
	for i, value := range values {
		if i > 0 && value == prev {
			continue
		}
		out = append(out, value)
		prev = value
	}
	return out
}
