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
	OpenBaoRuntimeCatalogSchemaVersion = 2
	DefaultNomadNamespace              = "default"
	TransitKeyTypeECDSAP256            = "ecdsa-p256"
)

type OpenBaoRuntimeCatalog struct {
	SchemaVersion    int                      `json:"schema_version"`
	GeneratedSecrets []OpenBaoGeneratedSecret `json:"generated_secrets"`
	TransitKeys      []OpenBaoTransitKey      `json:"transit_keys"`
	Roles            []OpenBaoRuntimeRole     `json:"roles"`
}

// OpenBaoTransitKey is a Transit signing key openbao-up must ensure exists.
// Exportable is always false; it is carried explicitly so the convergence
// side can refuse a catalog that ever asks for an exportable signing key.
type OpenBaoTransitKey struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Exportable bool   `json:"exportable"`
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

type OpenBaoRuntimeSecretDependency struct {
	SecretName    string
	ProducerJobID string
	ReaderJobID   string
}

type runtimeSecretClaim struct {
	path        string
	declaration RuntimeSecretDeclaration
}

type transitKeyClaim struct {
	path        string
	declaration TransitKeyDeclaration
}

type runtimeRoleBuilder struct {
	role  OpenBaoRuntimeRole
	paths map[string]map[string]bool
}

func OpenBaoRuntimeCatalogFromFiles(paths []string) (OpenBaoRuntimeCatalog, error) {
	claims, transitClaims, err := openBaoRuntimeClaimsFromFiles(paths)
	if err != nil {
		return OpenBaoRuntimeCatalog{}, err
	}
	if len(claims) == 0 && len(transitClaims) == 0 {
		return OpenBaoRuntimeCatalog{}, errors.New("at least one OpenBao runtime declaration is required")
	}
	return openBaoRuntimeCatalogFromClaims(claims, transitClaims)
}

func OpenBaoRuntimeSecretDependenciesFromFiles(paths []string) ([]OpenBaoRuntimeSecretDependency, error) {
	claims, _, err := openBaoRuntimeClaimsFromFiles(paths)
	if err != nil {
		return nil, err
	}
	dependencies := []OpenBaoRuntimeSecretDependency{}
	seen := map[OpenBaoRuntimeSecretDependency]bool{}
	for _, claim := range claims {
		declaration := normalizeRuntimeSecretDeclaration(claim.declaration)
		if err := validateRuntimeSecretDeclaration(claim.path, declaration); err != nil {
			return nil, err
		}
		if declaration.ProducedByJob == "" {
			continue
		}
		for _, reader := range runtimeSecretReaderJobIDs(declaration) {
			if reader == declaration.ProducedByJob {
				continue
			}
			dependency := OpenBaoRuntimeSecretDependency{
				SecretName:    declaration.Name,
				ProducerJobID: declaration.ProducedByJob,
				ReaderJobID:   reader,
			}
			if seen[dependency] {
				continue
			}
			seen[dependency] = true
			dependencies = append(dependencies, dependency)
		}
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].ProducerJobID != dependencies[j].ProducerJobID {
			return dependencies[i].ProducerJobID < dependencies[j].ProducerJobID
		}
		if dependencies[i].ReaderJobID != dependencies[j].ReaderJobID {
			return dependencies[i].ReaderJobID < dependencies[j].ReaderJobID
		}
		return dependencies[i].SecretName < dependencies[j].SecretName
	})
	return dependencies, nil
}

func openBaoRuntimeClaimsFromFiles(paths []string) ([]runtimeSecretClaim, []transitKeyClaim, error) {
	claims := []runtimeSecretClaim{}
	transitClaims := []transitKeyClaim{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || filepath.Base(path) != "runtime-secrets.yml" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", path, err)
		}
		var doc RuntimeSecretsFile
		dec := yaml.NewDecoder(bytes.NewReader(body))
		dec.KnownFields(true)
		if err := dec.Decode(&doc); err != nil {
			return nil, nil, fmt.Errorf("decode %s: %w", path, err)
		}
		for _, declaration := range doc.Declarations {
			claims = append(claims, runtimeSecretClaim{path: path, declaration: declaration})
		}
		for _, key := range doc.TransitKeys {
			transitClaims = append(transitClaims, transitKeyClaim{path: path, declaration: key})
		}
	}
	return claims, transitClaims, nil
}

func openBaoRuntimeCatalogFromClaims(claims []runtimeSecretClaim, transitClaims []transitKeyClaim) (OpenBaoRuntimeCatalog, error) {
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
	transitKeys := []OpenBaoTransitKey{}
	seenTransitKeys := map[string]string{}
	for _, claim := range transitClaims {
		declaration := normalizeTransitKeyDeclaration(claim.declaration)
		if err := validateTransitKeyDeclaration(claim.path, declaration); err != nil {
			return OpenBaoRuntimeCatalog{}, err
		}
		if previous := seenTransitKeys[declaration.Name]; previous != "" {
			return OpenBaoRuntimeCatalog{}, fmt.Errorf("%s: duplicate OpenBao transit key %q first declared by %s", claim.path, declaration.Name, previous)
		}
		seenTransitKeys[declaration.Name] = claim.path
		transitKeys = append(transitKeys, OpenBaoTransitKey{
			Name:       declaration.Name,
			Type:       declaration.Type,
			Exportable: false,
		})
		// The signer needs transit/keys read too: the sigstore hashivault
		// provider eagerly fetches the public key when it opens.
		addRuntimeRolePath(builders, declaration.JobID, transitKeysPath(declaration.Name), "read")
		addRuntimeRolePath(builders, declaration.JobID, transitSignPath(declaration.Name), "update")
		for _, jobID := range declaration.ConsumerJobIDs {
			addRuntimeRolePath(builders, jobID, transitKeysPath(declaration.Name), "read")
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
	sort.Slice(transitKeys, func(i, j int) bool {
		return transitKeys[i].Name < transitKeys[j].Name
	})
	return OpenBaoRuntimeCatalog{
		SchemaVersion:    OpenBaoRuntimeCatalogSchemaVersion,
		GeneratedSecrets: generated,
		TransitKeys:      transitKeys,
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
		case "base64url", "hex", "alphanumeric", "password":
		default:
			return fmt.Errorf("%s: runtime secret %s generated.encoding must be base64url, hex, alphanumeric, or password", path, declaration.Name)
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

func normalizeTransitKeyDeclaration(declaration TransitKeyDeclaration) TransitKeyDeclaration {
	declaration.Name = strings.TrimSpace(declaration.Name)
	declaration.Type = strings.TrimSpace(declaration.Type)
	declaration.JobID = strings.TrimSpace(declaration.JobID)
	for index, jobID := range declaration.ConsumerJobIDs {
		declaration.ConsumerJobIDs[index] = strings.TrimSpace(jobID)
	}
	return declaration
}

func validateTransitKeyDeclaration(path string, declaration TransitKeyDeclaration) error {
	if !jobIDRE.MatchString(declaration.Name) {
		return fmt.Errorf("%s: transit key name %q must match %s", path, declaration.Name, jobIDRE.String())
	}
	if declaration.Type != TransitKeyTypeECDSAP256 {
		return fmt.Errorf("%s: transit key %s type must be %s", path, declaration.Name, TransitKeyTypeECDSAP256)
	}
	if !jobIDRE.MatchString(declaration.JobID) {
		return fmt.Errorf("%s: transit key %s requires job_id matching %s", path, declaration.Name, jobIDRE.String())
	}
	seen := map[string]bool{declaration.JobID: true}
	for _, jobID := range declaration.ConsumerJobIDs {
		if !jobIDRE.MatchString(jobID) {
			return fmt.Errorf("%s: transit key %s has invalid consumer job_id %q", path, declaration.Name, jobID)
		}
		if seen[jobID] {
			return fmt.Errorf("%s: transit key %s repeats consumer job_id %q", path, declaration.Name, jobID)
		}
		seen[jobID] = true
	}
	return nil
}

// transitSignPath includes the /sha2-256 suffix: the sigstore hashivault
// provider signs at transit/sign/<name>/sha2-256, and OpenBao ACL paths are
// exact-match, so a grant on transit/sign/<name> alone is a 403.
func transitSignPath(name string) string {
	return "transit/sign/" + name + "/sha2-256"
}

func transitKeysPath(name string) string {
	return "transit/keys/" + name
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
