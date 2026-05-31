package deploycontract

import (
	"fmt"
	"strings"
)

type PostgresFile struct {
	ServiceDatabases    []PostgresServiceDatabase    `yaml:"postgresql_service_databases"`
	ReplicationRoles    []PostgresReplicationRole    `yaml:"postgresql_replication_roles"`
	LogicalPublications []PostgresLogicalPublication `yaml:"postgresql_logical_publications"`
	PeerMappings        []PostgresPeerMapping        `yaml:"postgresql_peer_mappings"`
}

type PostgresServiceDatabase struct {
	Name  string `yaml:"name"`
	Owner string `yaml:"owner"`
}

type PostgresReplicationRole struct {
	Name            string `yaml:"name"`
	ConnectionLimit int    `yaml:"connection_limit"`
	OpenBaoSecret   string `yaml:"openbao_secret"`
}

type PostgresLogicalPublication struct {
	Database         string   `yaml:"database"`
	Publication      string   `yaml:"publication"`
	PublicationOwner string   `yaml:"publication_owner"`
	TableOwner       string   `yaml:"table_owner"`
	ReplicationRole  string   `yaml:"replication_role"`
	Tables           []string `yaml:"tables"`
}

type PostgresPeerMapping struct {
	SystemUser    string `yaml:"system_user"`
	PGUser        string `yaml:"pg_user"`
	Purpose       string `yaml:"purpose"`
	Justification string `yaml:"justification"`
}

type RuntimeSecretsFile struct {
	Seeds []RuntimeSecretSeed `yaml:"openbao_runtime_secret_seed_declarations"`
}

type RuntimeSecretSeed struct {
	Name       string `yaml:"name"`
	JobID      string `yaml:"job_id"`
	SiteSecret string `yaml:"site_secret"`
	File       string `yaml:"file"`
	Generated  struct {
		Bytes int `yaml:"bytes"`
	} `yaml:"generated"`
}

type CredstoreFile struct {
	Files []CredstoreSecretFile `yaml:"credstore_secret_files"`
}

type CredstoreSecretFile struct {
	Path       string `yaml:"path"`
	Group      string `yaml:"group"`
	SiteSecret string `yaml:"site_secret"`
	Mode       string `yaml:"mode"`
}

type PublicRoutesFile struct {
	Routes []PublicRoute `yaml:"haproxy_public_routes"`
	APIs   []PublicAPI   `yaml:"haproxy_public_apis"`
}

type PublicRoute struct {
	Host    string `yaml:"host"`
	Backend string `yaml:"backend"`
}

type PublicAPI struct {
	Key        string `yaml:"key"`
	Host       string `yaml:"host"`
	PathPrefix string `yaml:"path_prefix"`
}

type ClickHouseClientFile struct {
	CACredentials []ClickHouseCACredential `yaml:"clickhouse_client_ca_credentials"`
}

type ClickHouseCACredential struct {
	Path  string `yaml:"path"`
	Group string `yaml:"group"`
}

type GatesFile struct {
	Gates []PromotionGate `yaml:"deployment_promotion_gates"`
}

type PromotionGate struct {
	Name          string   `yaml:"name"`
	Kind          string   `yaml:"kind"`
	Size          string   `yaml:"size"`
	EvidenceQuery string   `yaml:"evidence_query"`
	DependsOn     []string `yaml:"depends_on"`
	Timeout       string   `yaml:"timeout"`
}

func (v *Validator) validatePostgres(rel string, doc PostgresFile) {
	v.report.PostgresFiles++
	for i, db := range doc.ServiceDatabases {
		prefix := fmt.Sprintf("postgresql_service_databases[%d]", i)
		requireName(v, rel, prefix+".name", db.Name)
		requireName(v, rel, prefix+".owner", db.Owner)
		v.claim(rel, v.seen.postgresDatabases, "PostgreSQL database", db.Name)
		v.claim(rel, v.seen.postgresOwners, "PostgreSQL owner role", db.Owner)
	}
	for i, role := range doc.ReplicationRoles {
		prefix := fmt.Sprintf("postgresql_replication_roles[%d]", i)
		requireName(v, rel, prefix+".name", role.Name)
		if role.ConnectionLimit <= 0 {
			v.add(rel, prefix+".connection_limit must be positive")
		}
		if !secretRE.MatchString(strings.TrimSpace(role.OpenBaoSecret)) {
			v.add(rel, fmt.Sprintf("%s.openbao_secret must match %s", prefix, secretRE.String()))
		}
		v.claim(rel, v.seen.replicationRoles, "PostgreSQL replication role", role.Name)
	}
	for i, mapping := range doc.PeerMappings {
		prefix := fmt.Sprintf("postgresql_peer_mappings[%d]", i)
		requireName(v, rel, prefix+".system_user", mapping.SystemUser)
		requireName(v, rel, prefix+".pg_user", mapping.PGUser)
		v.seen.peerMappings = append(v.seen.peerMappings, postgresPeerClaim{
			path:          rel,
			systemUser:    mapping.SystemUser,
			pgUser:        mapping.PGUser,
			purpose:       mapping.Purpose,
			justification: mapping.Justification,
		})
	}
	for i, pub := range doc.LogicalPublications {
		prefix := fmt.Sprintf("postgresql_logical_publications[%d]", i)
		requireName(v, rel, prefix+".database", pub.Database)
		requireName(v, rel, prefix+".publication", pub.Publication)
		requireName(v, rel, prefix+".publication_owner", pub.PublicationOwner)
		requireName(v, rel, prefix+".table_owner", pub.TableOwner)
		requireName(v, rel, prefix+".replication_role", pub.ReplicationRole)
		if len(pub.Tables) == 0 {
			v.add(rel, prefix+".tables must not be empty")
		}
		for j, table := range pub.Tables {
			requireName(v, rel, fmt.Sprintf("%s.tables[%d]", prefix, j), table)
		}
	}
}

func (v *Validator) validatePostgresPeerMappings() {
	for _, mapping := range v.seen.peerMappings {
		if mapping.systemUser == "" || mapping.pgUser == "" {
			continue
		}
		if mapping.systemUser == mapping.pgUser || (mapping.systemUser == "postgres" && mapping.pgUser == "postgres") {
			continue
		}
		ownerPath := v.seen.postgresOwners[mapping.pgUser]
		if ownerPath == "" || ownerPath == mapping.path {
			continue
		}
		if mapping.purpose != "data_export" || strings.TrimSpace(mapping.justification) == "" {
			v.add(mapping.path, fmt.Sprintf("cross-service postgres peer mapping %s -> %s must declare purpose: data_export and justification", mapping.systemUser, mapping.pgUser))
		}
	}
}

func (v *Validator) validateRuntimeSecrets(rel string, doc RuntimeSecretsFile) {
	for i, seed := range doc.Seeds {
		v.report.RuntimeSecrets++
		prefix := fmt.Sprintf("openbao_runtime_secret_seed_declarations[%d]", i)
		if !secretRE.MatchString(strings.TrimSpace(seed.Name)) {
			v.add(rel, fmt.Sprintf("%s.name must match %s", prefix, secretRE.String()))
		}
		if !exactlyOneRuntimeSecretSource(seed.SiteSecret, seed.File, seed.Generated.Bytes) {
			v.add(rel, prefix+" must declare exactly one source: site_secret, file, or generated")
		}
		if seed.SiteSecret != "" {
			requireName(v, rel, prefix+".site_secret", seed.SiteSecret)
		}
		if seed.File != "" {
			requirePathPrefix(v, rel, prefix+".file", seed.File, "/")
		}
		if seed.Generated.Bytes != 0 && (seed.Generated.Bytes < 16 || seed.Generated.Bytes > 96) {
			v.add(rel, prefix+".generated.bytes must be between 16 and 96")
		}
		v.claim(rel, v.seen.runtimeSecrets, "OpenBao runtime secret", seed.Name)
	}
}

func (v *Validator) validateCredstore(rel string, doc CredstoreFile) {
	for i, file := range doc.Files {
		v.report.CredstoreFiles++
		prefix := fmt.Sprintf("credstore_secret_files[%d]", i)
		requirePathPrefix(v, rel, prefix+".path", file.Path, "/etc/credstore/")
		requireName(v, rel, prefix+".group", file.Group)
		requireName(v, rel, prefix+".site_secret", file.SiteSecret)
		if file.Mode == "" {
			v.add(rel, prefix+".mode is required")
		} else if !modeRE.MatchString(file.Mode) {
			v.add(rel, prefix+".mode must be a four-digit octal mode")
		}
		v.claim(rel, v.seen.credstorePaths, "credstore path", file.Path)
	}
}

func (v *Validator) validatePublicRoutes(rel string, doc PublicRoutesFile) {
	for i, route := range doc.Routes {
		v.report.PublicRoutes++
		prefix := fmt.Sprintf("haproxy_public_routes[%d]", i)
		if strings.TrimSpace(route.Host) == "" {
			v.add(rel, prefix+".host is required")
		}
		if !strings.HasPrefix(strings.TrimSpace(route.Backend), "be_") {
			v.add(rel, prefix+".backend must name an HAProxy backend")
		}
		v.claim(rel, v.seen.publicRouteHosts, "public route host", route.Host)
	}
	for i, api := range doc.APIs {
		v.report.PublicAPIs++
		prefix := fmt.Sprintf("haproxy_public_apis[%d]", i)
		if !apiKeyRE.MatchString(strings.TrimSpace(api.Key)) {
			v.add(rel, prefix+".key must be a stable API key")
		}
		if strings.TrimSpace(api.Host) == "" {
			v.add(rel, prefix+".host is required")
		}
		if !strings.HasPrefix(strings.TrimSpace(api.PathPrefix), "/") {
			v.add(rel, prefix+".path_prefix must start with /")
		}
		v.claim(rel, v.seen.publicAPIKeys, "public API key", api.Key)
	}
}

func (v *Validator) validateClickHouse(rel string, doc ClickHouseClientFile) {
	for i, cred := range doc.CACredentials {
		v.report.ClickHouseCreds++
		prefix := fmt.Sprintf("clickhouse_client_ca_credentials[%d]", i)
		if !strings.HasPrefix(strings.TrimSpace(cred.Path), "/etc/credstore/") && !strings.HasPrefix(strings.TrimSpace(cred.Path), "/etc/otelcol/") {
			v.add(rel, prefix+".path must start with /etc/credstore/ or /etc/otelcol/")
		}
		requireName(v, rel, prefix+".group", cred.Group)
		v.claim(rel, v.seen.clickhouseCreds, "ClickHouse CA credential path", cred.Path)
	}
}

func (v *Validator) validateGates(rel string, doc GatesFile) {
	for i, gate := range doc.Gates {
		v.report.Gates++
		prefix := fmt.Sprintf("deployment_promotion_gates[%d]", i)
		requireName(v, rel, prefix+".name", gate.Name)
		switch gate.Kind {
		case "api", "browser", "cli", "clickhouse_query", "provider":
		default:
			v.add(rel, prefix+".kind must be api, browser, cli, clickhouse_query, or provider")
		}
		switch gate.Size {
		case "medium", "large":
		default:
			v.add(rel, prefix+".size must be medium or large")
		}
		if strings.TrimSpace(gate.EvidenceQuery) == "" {
			v.add(rel, prefix+".evidence_query is required")
		}
		if gate.Timeout != "" && !durationRE.MatchString(gate.Timeout) {
			v.add(rel, prefix+".timeout must be a Go-style duration using ms, s, m, or h")
		}
		for j, dep := range gate.DependsOn {
			requireName(v, rel, fmt.Sprintf("%s.depends_on[%d]", prefix, j), dep)
		}
		v.claim(rel, v.seen.gateNames, "deployment promotion gate", rel+":"+gate.Name)
	}
}
