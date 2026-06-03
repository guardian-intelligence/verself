package sitebootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/verself/deployment-service/deploycontract"
	"gopkg.in/yaml.v3"
)

type postgresCatalog struct {
	ServiceDatabases []deploycontract.PostgresServiceDatabase
	ReplicationRoles []deploycontract.PostgresReplicationRole
	PeerMappings     []deploycontract.PostgresPeerMapping
}

type runtimeSecretCatalog struct {
	Declarations []runtimeSecretDeclaration
	ByName       map[string]runtimeSecretDeclaration
}

type runtimeSecretDeclaration struct {
	Name            string
	JobID           string
	ConsumerJobIDs  []string
	ProducedByJob   string
	ExternalOpenBao bool
	GeneratedBytes  int
	GeneratedFormat string
}

func loadPostgresCatalog(repoRoot string) (postgresCatalog, error) {
	files, err := findDeployFiles(repoRoot, "postgres.yml")
	if err != nil {
		return postgresCatalog{}, err
	}
	catalog := postgresCatalog{}
	databases := map[string]deploycontract.PostgresServiceDatabase{}
	replicationRoles := map[string]deploycontract.PostgresReplicationRole{}
	peerMappings := map[string]deploycontract.PostgresPeerMapping{}
	for _, file := range files {
		var doc deploycontract.PostgresFile
		if err := decodeYAMLFile(file, &doc); err != nil {
			return postgresCatalog{}, err
		}
		for _, db := range doc.ServiceDatabases {
			db.Name = strings.TrimSpace(db.Name)
			db.Owner = strings.TrimSpace(db.Owner)
			if db.Name == "" || db.Owner == "" {
				return postgresCatalog{}, fmt.Errorf("%s: PostgreSQL service database requires name and owner", file)
			}
			databases[db.Name] = db
		}
		for _, role := range doc.ReplicationRoles {
			role.Name = strings.TrimSpace(role.Name)
			role.OpenBaoSecret = strings.TrimSpace(role.OpenBaoSecret)
			if role.Name == "" || role.OpenBaoSecret == "" {
				return postgresCatalog{}, fmt.Errorf("%s: PostgreSQL replication role requires name and openbao_secret", file)
			}
			replicationRoles[role.Name] = role
		}
		for _, mapping := range doc.PeerMappings {
			mapping.SystemUser = strings.TrimSpace(mapping.SystemUser)
			mapping.PGUser = strings.TrimSpace(mapping.PGUser)
			if mapping.SystemUser == "" || mapping.PGUser == "" {
				return postgresCatalog{}, fmt.Errorf("%s: PostgreSQL peer mapping requires system_user and pg_user", file)
			}
			peerMappings[mapping.SystemUser+"\x00"+mapping.PGUser] = mapping
		}
	}
	peerMappings["postgres\x00postgres"] = deploycontract.PostgresPeerMapping{SystemUser: "postgres", PGUser: "postgres"}
	for _, db := range sortedPostgresDatabases(databases) {
		catalog.ServiceDatabases = append(catalog.ServiceDatabases, db)
	}
	for _, role := range sortedReplicationRoles(replicationRoles) {
		catalog.ReplicationRoles = append(catalog.ReplicationRoles, role)
	}
	for _, mapping := range sortedPeerMappings(peerMappings) {
		catalog.PeerMappings = append(catalog.PeerMappings, mapping)
	}
	return catalog, nil
}

func loadRuntimeSecretCatalog(repoRoot string) (runtimeSecretCatalog, error) {
	files, err := findDeployFiles(repoRoot, "runtime-secrets.yml")
	if err != nil {
		return runtimeSecretCatalog{}, err
	}
	catalog := runtimeSecretCatalog{ByName: map[string]runtimeSecretDeclaration{}}
	for _, file := range files {
		var doc deploycontract.RuntimeSecretsFile
		if err := decodeYAMLFile(file, &doc); err != nil {
			return runtimeSecretCatalog{}, err
		}
		for _, raw := range doc.Declarations {
			declaration := runtimeSecretDeclaration{
				Name:            strings.TrimSpace(raw.Name),
				JobID:           strings.TrimSpace(raw.JobID),
				ConsumerJobIDs:  trimStringSlice(raw.ConsumerJobIDs),
				ProducedByJob:   strings.TrimSpace(raw.ProducedByJob),
				ExternalOpenBao: raw.ExternalOpenBao,
				GeneratedBytes:  raw.Generated.Bytes,
				GeneratedFormat: strings.TrimSpace(raw.Generated.Encoding),
			}
			if declaration.Name == "" {
				return runtimeSecretCatalog{}, fmt.Errorf("%s: OpenBao runtime secret declaration requires name", file)
			}
			if _, exists := catalog.ByName[declaration.Name]; exists {
				return runtimeSecretCatalog{}, fmt.Errorf("%s: duplicate OpenBao runtime secret declaration %s", file, declaration.Name)
			}
			catalog.ByName[declaration.Name] = declaration
			catalog.Declarations = append(catalog.Declarations, declaration)
		}
	}
	sort.Slice(catalog.Declarations, func(i, j int) bool {
		return catalog.Declarations[i].Name < catalog.Declarations[j].Name
	})
	return catalog, nil
}

func (d runtimeSecretDeclaration) readerPrincipals() []string {
	out := []string{}
	if strings.TrimSpace(d.JobID) != "" {
		out = append(out, strings.TrimSpace(d.JobID))
	}
	out = append(out, trimStringSlice(d.ConsumerJobIDs)...)
	return out
}

func (d runtimeSecretDeclaration) writerPrincipals() []string {
	if strings.TrimSpace(d.ProducedByJob) == "" {
		return nil
	}
	return []string{strings.TrimSpace(d.ProducedByJob)}
}

func findDeployFiles(repoRoot, name string) ([]string, error) {
	root := filepath.Join(repoRoot, "src")
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "bazel-bin", "bazel-out", "bazel-testlogs", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == name && filepath.Base(filepath.Dir(path)) == "deploy" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find deploy/%s files: %w", name, err)
	}
	sort.Strings(files)
	return files, nil
}

func decodeYAMLFile(path string, out any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func trimStringSlice(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func sortedPostgresDatabases(values map[string]deploycontract.PostgresServiceDatabase) []deploycontract.PostgresServiceDatabase {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]deploycontract.PostgresServiceDatabase, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func sortedReplicationRoles(values map[string]deploycontract.PostgresReplicationRole) []deploycontract.PostgresReplicationRole {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]deploycontract.PostgresReplicationRole, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}

func sortedPeerMappings(values map[string]deploycontract.PostgresPeerMapping) []deploycontract.PostgresPeerMapping {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]deploycontract.PostgresPeerMapping, 0, len(keys))
	for _, key := range keys {
		out = append(out, values[key])
	}
	return out
}
