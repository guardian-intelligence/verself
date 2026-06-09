package deploycontract

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	OpenBaoRuntimeCatalogSchemaVersion = 1
	DefaultNomadNamespace              = "default"
)

type OpenBaoRuntimeCatalog struct {
	SchemaVersion    int                      `json:"schema_version"`
	GeneratedSecrets []OpenBaoGeneratedSecret `json:"generated_secrets"`
	Roles            []OpenBaoRuntimeRole     `json:"roles"`
}

type OpenBaoGeneratedSecret struct {
	Name     string `json:"name"`
	Bytes    int    `json:"bytes"`
	Encoding string `json:"encoding"`
}

type OpenBaoRuntimeRole struct {
	Name           string                     `json:"name"`
	NomadNamespace string                     `json:"nomad_namespace"`
	JobID          string                     `json:"job_id"`
	Paths          []OpenBaoRuntimePolicyPath `json:"paths"`
}

type OpenBaoRuntimePolicyPath struct {
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"`
}

type runtimeSecretClaim struct {
	path        string
	declaration RuntimeSecretDeclaration
}

type runtimeRoleBuilder struct {
	role  OpenBaoRuntimeRole
	paths map[string]map[string]bool
}

func OpenBaoRuntimeCatalogFromFiles(paths []string) (OpenBaoRuntimeCatalog, error) {
	claims := []runtimeSecretClaim{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || filepath.Base(path) != "runtime-secrets.yml" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return OpenBaoRuntimeCatalog{}, fmt.Errorf("read %s: %w", path, err)
		}
		var doc RuntimeSecretsFile
		dec := yaml.NewDecoder(bytes.NewReader(body))
		dec.KnownFields(true)
		if err := dec.Decode(&doc); err != nil {
			return OpenBaoRuntimeCatalog{}, fmt.Errorf("decode %s: %w", path, err)
		}
		for _, declaration := range doc.Declarations {
			claims = append(claims, runtimeSecretClaim{path: path, declaration: declaration})
		}
	}
	if len(claims) == 0 {
		return OpenBaoRuntimeCatalog{}, errors.New("at least one OpenBao runtime secret declaration is required")
	}
	return openBaoRuntimeCatalogFromClaims(claims)
}

func openBaoRuntimeCatalogFromClaims(claims []runtimeSecretClaim) (OpenBaoRuntimeCatalog, error) {
	builders := map[string]*runtimeRoleBuilder{}
	generated := []OpenBaoGeneratedSecret{}
	seenSecrets := map[string]string{}
	for _, claim := range claims {
		declaration := normalizeRuntimeSecretDeclaration(claim.declaration)
		if err := validateRuntimeSecretDeclaration(claim.path, declaration); err != nil {
			return OpenBaoRuntimeCatalog{}, err
		}
		if previous := seenSecrets[declaration.Name]; previous != "" {
			return OpenBaoRuntimeCatalog{}, fmt.Errorf("%s: duplicate OpenBao runtime secret %q first declared by %s", claim.path, declaration.Name, previous)
		}
		seenSecrets[declaration.Name] = claim.path
		if declaration.Generated.Bytes != 0 {
			generated = append(generated, OpenBaoGeneratedSecret{
				Name:     declaration.Name,
				Bytes:    declaration.Generated.Bytes,
				Encoding: declaration.Generated.Encoding,
			})
		}
		for _, jobID := range runtimeSecretReaderJobIDs(declaration) {
			addRuntimeRolePath(builders, jobID, runtimeSecretDataPath(declaration.Name), "read")
		}
		if declaration.ProducedByJob != "" {
			addRuntimeRolePath(builders, declaration.ProducedByJob, runtimeSecretDataPath(declaration.Name), "create", "read", "update")
		}
	}
	roles := make([]OpenBaoRuntimeRole, 0, len(builders))
	for _, builder := range builders {
		for path, caps := range builder.paths {
			builder.role.Paths = append(builder.role.Paths, OpenBaoRuntimePolicyPath{
				Path:         path,
				Capabilities: orderedCapabilities(caps),
			})
		}
		sort.Slice(builder.role.Paths, func(i, j int) bool {
			return builder.role.Paths[i].Path < builder.role.Paths[j].Path
		})
		roles = append(roles, builder.role)
	}
	sort.Slice(roles, func(i, j int) bool {
		return roles[i].Name < roles[j].Name
	})
	sort.Slice(generated, func(i, j int) bool {
		return generated[i].Name < generated[j].Name
	})
	return OpenBaoRuntimeCatalog{
		SchemaVersion:    OpenBaoRuntimeCatalogSchemaVersion,
		GeneratedSecrets: generated,
		Roles:            roles,
	}, nil
}

func normalizeRuntimeSecretDeclaration(declaration RuntimeSecretDeclaration) RuntimeSecretDeclaration {
	declaration.Name = strings.TrimSpace(declaration.Name)
	declaration.JobID = strings.TrimSpace(declaration.JobID)
	declaration.ProducedByJob = strings.TrimSpace(declaration.ProducedByJob)
	declaration.Generated.Encoding = strings.TrimSpace(declaration.Generated.Encoding)
	for index, jobID := range declaration.ConsumerJobIDs {
		declaration.ConsumerJobIDs[index] = strings.TrimSpace(jobID)
	}
	return declaration
}

func validateRuntimeSecretDeclaration(path string, declaration RuntimeSecretDeclaration) error {
	if !secretRE.MatchString(declaration.Name) {
		return fmt.Errorf("%s: runtime secret name %q must match %s", path, declaration.Name, secretRE.String())
	}
	if !jobIDRE.MatchString(declaration.JobID) {
		return fmt.Errorf("%s: runtime secret %s requires job_id matching %s", path, declaration.Name, jobIDRE.String())
	}
	if !exactlyOneRuntimeSecretSource(declaration.ProducedByJob, declaration.Generated.Bytes, declaration.ExternalOpenBao) {
		return fmt.Errorf("%s: runtime secret %s must declare exactly one source: generated, produced_by_job, or external_openbao", path, declaration.Name)
	}
	for _, jobID := range declaration.ConsumerJobIDs {
		if !jobIDRE.MatchString(jobID) {
			return fmt.Errorf("%s: runtime secret %s has invalid consumer job_id %q", path, declaration.Name, jobID)
		}
	}
	if declaration.ProducedByJob != "" && !jobIDRE.MatchString(declaration.ProducedByJob) {
		return fmt.Errorf("%s: runtime secret %s has invalid produced_by_job %q", path, declaration.Name, declaration.ProducedByJob)
	}
	if declaration.Generated.Bytes != 0 {
		if declaration.Generated.Bytes < 16 || declaration.Generated.Bytes > 96 {
			return fmt.Errorf("%s: runtime secret %s generated.bytes must be between 16 and 96", path, declaration.Name)
		}
		switch declaration.Generated.Encoding {
		case "base64url", "hex", "alphanumeric":
		default:
			return fmt.Errorf("%s: runtime secret %s generated.encoding must be base64url, hex, or alphanumeric", path, declaration.Name)
		}
	}
	seen := map[string]bool{declaration.JobID: true}
	for _, jobID := range declaration.ConsumerJobIDs {
		if seen[jobID] {
			return fmt.Errorf("%s: runtime secret %s repeats reader job_id %q", path, declaration.Name, jobID)
		}
		seen[jobID] = true
	}
	return nil
}

func runtimeSecretReaderJobIDs(declaration RuntimeSecretDeclaration) []string {
	readers := []string{declaration.JobID}
	readers = append(readers, declaration.ConsumerJobIDs...)
	sort.Strings(readers)
	return readers
}

func addRuntimeRolePath(builders map[string]*runtimeRoleBuilder, jobID string, path string, capabilities ...string) {
	roleName := jobID + "-runtime"
	builder := builders[roleName]
	if builder == nil {
		builder = &runtimeRoleBuilder{
			role: OpenBaoRuntimeRole{
				Name:           roleName,
				NomadNamespace: DefaultNomadNamespace,
				JobID:          jobID,
			},
			paths: map[string]map[string]bool{},
		}
		builders[roleName] = builder
	}
	if builder.paths[path] == nil {
		builder.paths[path] = map[string]bool{}
	}
	for _, capability := range capabilities {
		builder.paths[path][capability] = true
	}
}

func runtimeSecretDataPath(name string) string {
	return "kv-runtime/data/secret/org/" + name
}

func orderedCapabilities(caps map[string]bool) []string {
	order := []string{"create", "read", "update"}
	out := make([]string, 0, len(caps))
	for _, capability := range order {
		if caps[capability] {
			out = append(out, capability)
		}
	}
	return out
}
