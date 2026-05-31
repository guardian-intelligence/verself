package postgresruntime

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

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type Plan struct {
	Databases        []Database
	ReplicationRoles []ReplicationRole
	PeerMappings     []PeerMapping
	Publications     []Publication
}

type Database struct {
	Name  string
	Owner string
	Path  string
}

type ReplicationRole struct {
	Name            string
	ConnectionLimit int
	OpenBaoSecret   string
	Path            string
}

type PeerMapping struct {
	SystemUser string
	PGUser     string
	Path       string
}

type Publication struct {
	Database         string
	Publication      string
	PublicationOwner string
	TableOwner       string
	ReplicationRole  string
	Tables           []string
	Path             string
}

func LoadPlan(repoRoot string) (Plan, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return Plan{}, fmt.Errorf("repo root is required")
	}
	plan := Plan{
		PeerMappings: []PeerMapping{{SystemUser: "postgres", PGUser: "postgres", Path: "postgresql:base"}},
	}
	for _, relRoot := range []string{"src/infrastructure-components", "src/services", "src/viteplus-monorepo/apps"} {
		root := filepath.Join(repoRoot, filepath.FromSlash(relRoot))
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Plan{}, fmt.Errorf("stat %s: %w", relRoot, err)
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "postgres.yml" || filepath.Base(filepath.Dir(path)) != "deploy" {
				return nil
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			doc, err := readPostgresFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			for _, db := range doc.ServiceDatabases {
				plan.Databases = append(plan.Databases, Database{Name: db.Name, Owner: db.Owner, Path: rel})
			}
			for _, role := range doc.ReplicationRoles {
				plan.ReplicationRoles = append(plan.ReplicationRoles, ReplicationRole{
					Name:            role.Name,
					ConnectionLimit: role.ConnectionLimit,
					OpenBaoSecret:   role.OpenBaoSecret,
					Path:            rel,
				})
			}
			for _, mapping := range doc.PeerMappings {
				plan.PeerMappings = append(plan.PeerMappings, PeerMapping{SystemUser: mapping.SystemUser, PGUser: mapping.PGUser, Path: rel})
			}
			for _, pub := range doc.LogicalPublications {
				plan.Publications = append(plan.Publications, Publication{
					Database:         pub.Database,
					Publication:      pub.Publication,
					PublicationOwner: pub.PublicationOwner,
					TableOwner:       pub.TableOwner,
					ReplicationRole:  pub.ReplicationRole,
					Tables:           append([]string(nil), pub.Tables...),
					Path:             rel,
				})
			}
			return nil
		})
		if err != nil {
			return Plan{}, fmt.Errorf("walk %s: %w", relRoot, err)
		}
	}
	normalize(&plan)
	if err := validate(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func readPostgresFile(path string) (deploycontract.PostgresFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return deploycontract.PostgresFile{}, fmt.Errorf("read: %w", err)
	}
	var doc deploycontract.PostgresFile
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return deploycontract.PostgresFile{}, fmt.Errorf("decode yaml: %w", err)
	}
	return doc, nil
}

func normalize(plan *Plan) {
	sort.Slice(plan.Databases, func(i, j int) bool {
		if plan.Databases[i].Name != plan.Databases[j].Name {
			return plan.Databases[i].Name < plan.Databases[j].Name
		}
		return plan.Databases[i].Path < plan.Databases[j].Path
	})
	sort.Slice(plan.ReplicationRoles, func(i, j int) bool { return plan.ReplicationRoles[i].Name < plan.ReplicationRoles[j].Name })
	sort.Slice(plan.PeerMappings, func(i, j int) bool {
		if plan.PeerMappings[i].SystemUser != plan.PeerMappings[j].SystemUser {
			return plan.PeerMappings[i].SystemUser < plan.PeerMappings[j].SystemUser
		}
		return plan.PeerMappings[i].PGUser < plan.PeerMappings[j].PGUser
	})
	sort.Slice(plan.Publications, func(i, j int) bool {
		if plan.Publications[i].Database != plan.Publications[j].Database {
			return plan.Publications[i].Database < plan.Publications[j].Database
		}
		return plan.Publications[i].Publication < plan.Publications[j].Publication
	})
	for i := range plan.Publications {
		sort.Strings(plan.Publications[i].Tables)
	}
}

func validate(plan Plan) error {
	seenDB := map[string]string{}
	seenRole := map[string]string{}
	seenMapping := map[string]string{}
	for _, db := range plan.Databases {
		if err := requireName(db.Path, "database name", db.Name); err != nil {
			return err
		}
		if err := requireName(db.Path, "database owner", db.Owner); err != nil {
			return err
		}
		if prior := seenDB[db.Name]; prior != "" && prior != db.Path {
			return fmt.Errorf("%s: PostgreSQL database %q is also declared in %s", db.Path, db.Name, prior)
		}
		seenDB[db.Name] = db.Path
	}
	for _, role := range plan.ReplicationRoles {
		if err := requireName(role.Path, "replication role", role.Name); err != nil {
			return err
		}
		if role.ConnectionLimit <= 0 {
			return fmt.Errorf("%s: replication role %s connection_limit must be positive", role.Path, role.Name)
		}
		if strings.TrimSpace(role.OpenBaoSecret) == "" {
			return fmt.Errorf("%s: replication role %s openbao_secret is required", role.Path, role.Name)
		}
		if prior := seenRole[role.Name]; prior != "" && prior != role.Path {
			return fmt.Errorf("%s: PostgreSQL replication role %q is also declared in %s", role.Path, role.Name, prior)
		}
		seenRole[role.Name] = role.Path
	}
	for _, mapping := range plan.PeerMappings {
		if err := requireName(mapping.Path, "peer mapping system user", mapping.SystemUser); err != nil {
			return err
		}
		if err := requireName(mapping.Path, "peer mapping PostgreSQL user", mapping.PGUser); err != nil {
			return err
		}
		key := mapping.SystemUser + "\x00" + mapping.PGUser
		if prior := seenMapping[key]; prior != "" {
			return fmt.Errorf("%s: PostgreSQL peer mapping %s -> %s is also declared in %s", mapping.Path, mapping.SystemUser, mapping.PGUser, prior)
		}
		seenMapping[key] = mapping.Path
	}
	for _, pub := range plan.Publications {
		for field, value := range map[string]string{
			"publication database":         pub.Database,
			"publication":                  pub.Publication,
			"publication owner":            pub.PublicationOwner,
			"publication table owner":      pub.TableOwner,
			"publication replication role": pub.ReplicationRole,
		} {
			if err := requireName(pub.Path, field, value); err != nil {
				return err
			}
		}
		if len(pub.Tables) == 0 {
			return fmt.Errorf("%s: publication %s must declare at least one table", pub.Path, pub.Publication)
		}
		for _, table := range pub.Tables {
			if err := requireName(pub.Path, "publication table", table); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireName(path, field, value string) error {
	if !nameRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s: %s %q must match %s", path, field, value, nameRE.String())
	}
	return nil
}
