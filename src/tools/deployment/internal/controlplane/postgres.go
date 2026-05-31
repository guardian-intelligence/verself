package controlplane

import (
	"fmt"
	"sort"
	"strings"
)

func validatePostgres(plan PostgresBundle) error {
	seenDB := map[string]string{}
	seenRole := map[string]string{}
	seenMapping := map[string]string{}
	for _, db := range plan.Databases {
		if err := requirePGName(db.Path, "database name", db.Name); err != nil {
			return err
		}
		if err := requirePGName(db.Path, "database owner", db.Owner); err != nil {
			return err
		}
		if prior := seenDB[db.Name]; prior != "" && prior != db.Path {
			return fmt.Errorf("%s: PostgreSQL database %q is also declared in %s", db.Path, db.Name, prior)
		}
		seenDB[db.Name] = db.Path
	}
	for _, role := range plan.ReplicationRoles {
		if err := requirePGName(role.Path, "replication role", role.Name); err != nil {
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
		if err := requirePGName(mapping.Path, "peer mapping system user", mapping.SystemUser); err != nil {
			return err
		}
		if err := requirePGName(mapping.Path, "peer mapping PostgreSQL user", mapping.PGUser); err != nil {
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
			if err := requirePGName(pub.Path, field, value); err != nil {
				return err
			}
		}
		if len(pub.Tables) == 0 {
			return fmt.Errorf("%s: publication %s must declare at least one table", pub.Path, pub.Publication)
		}
		for _, table := range pub.Tables {
			if err := requirePGName(pub.Path, "publication table", table); err != nil {
				return err
			}
		}
	}
	return nil
}

func requirePGName(path, field, value string) error {
	if !nameRE.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("%s: %s %q must match %s", path, field, value, nameRE.String())
	}
	return nil
}

func peerMap(mappings []PostgresPeerMapping) string {
	var b strings.Builder
	b.WriteString("# PostgreSQL user name maps.\n# Managed by substrate-control-plane.\n")
	for _, mapping := range mappings {
		fmt.Fprintf(&b, "verself_services      %-24s %s\n", mapping.SystemUser, mapping.PGUser)
	}
	return b.String()
}

func baseRoles(plan PostgresBundle) []string {
	seen := map[string]bool{"postgres": true}
	for _, db := range plan.Databases {
		seen[db.Owner] = true
	}
	for _, mapping := range plan.PeerMappings {
		seen[mapping.PGUser] = true
	}
	out := make([]string, 0, len(seen))
	for role := range seen {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func baseSQL(roles []string, dbs []PostgresDatabase) string {
	var b strings.Builder
	b.WriteString("SET client_min_messages TO warning;\n")
	for _, role := range roles {
		if role == "postgres" {
			continue
		}
		fmt.Fprintf(&b, "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN CREATE ROLE %s LOGIN; END IF; END $$;\n", quoteLiteral(role), quoteIdent(role))
		fmt.Fprintf(&b, "ALTER ROLE %s LOGIN;\n", quoteIdent(role))
	}
	for _, db := range dbs {
		fmt.Fprintf(&b, "SELECT format('CREATE DATABASE %%I OWNER %%I', %s, %s) WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = %s)\\gexec\n", quoteLiteral(db.Name), quoteLiteral(db.Owner), quoteLiteral(db.Name))
		fmt.Fprintf(&b, "ALTER DATABASE %s OWNER TO %s;\n", quoteIdent(db.Name), quoteIdent(db.Owner))
	}
	b.WriteString("SELECT pg_reload_conf();\n")
	return b.String()
}

func replicationRolesSQL(roles []PostgresReplicationRole, secrets map[string]string) string {
	var b strings.Builder
	b.WriteString("SET client_min_messages TO warning;\n")
	for _, role := range roles {
		fmt.Fprintf(&b, "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN CREATE ROLE %s LOGIN REPLICATION CONNECTION LIMIT %d; END IF; END $$;\n", quoteLiteral(role.Name), quoteIdent(role.Name), role.ConnectionLimit)
		fmt.Fprintf(&b, "ALTER ROLE %s WITH LOGIN REPLICATION CONNECTION LIMIT %d PASSWORD %s;\n", quoteIdent(role.Name), role.ConnectionLimit, quoteLiteral(secrets[role.Name]))
	}
	return b.String()
}

func publicationSQL(pub PostgresPublication) string {
	var b strings.Builder
	tableArray := sqlStringArray(pub.Tables)
	tableList := qualifiedTableList(pub.Tables)
	b.WriteString("SET client_min_messages TO warning;\n")
	b.WriteString("DO $verself_postgres$\n")
	b.WriteString("DECLARE missing text[];\n")
	b.WriteString("BEGIN\n")
	fmt.Fprintf(&b, "  SELECT array_agg(t) INTO missing FROM unnest(%s) AS t WHERE to_regclass('public.' || quote_ident(t)) IS NULL;\n", tableArray)
	b.WriteString("  IF COALESCE(array_length(missing, 1), 0) > 0 THEN\n")
	fmt.Fprintf(&b, "    RAISE EXCEPTION 'publication %% missing tables: %%', %s, missing;\n", quoteLiteral(pub.Publication))
	b.WriteString("  END IF;\n")
	for _, table := range pub.Tables {
		fmt.Fprintf(&b, "  EXECUTE %s;\n", quoteLiteral("ALTER TABLE public."+quoteIdent(table)+" OWNER TO "+quoteIdent(pub.TableOwner)))
	}
	fmt.Fprintf(&b, "  EXECUTE %s;\n", quoteLiteral("GRANT CONNECT ON DATABASE "+quoteIdent(pub.Database)+" TO "+quoteIdent(pub.ReplicationRole)))
	fmt.Fprintf(&b, "  EXECUTE %s;\n", quoteLiteral("GRANT USAGE ON SCHEMA public TO "+quoteIdent(pub.ReplicationRole)))
	fmt.Fprintf(&b, "  EXECUTE %s;\n", quoteLiteral("GRANT SELECT ON TABLE "+tableList+" TO "+quoteIdent(pub.ReplicationRole)))
	fmt.Fprintf(&b, "  IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = %s) THEN\n", quoteLiteral(pub.Publication))
	fmt.Fprintf(&b, "    EXECUTE %s;\n", quoteLiteral("CREATE PUBLICATION "+quoteIdent(pub.Publication)+" FOR TABLE "+tableList))
	b.WriteString("  ELSE\n")
	fmt.Fprintf(&b, "    EXECUTE %s;\n", quoteLiteral("ALTER PUBLICATION "+quoteIdent(pub.Publication)+" SET TABLE "+tableList))
	b.WriteString("  END IF;\n")
	fmt.Fprintf(&b, "  EXECUTE %s;\n", quoteLiteral("ALTER PUBLICATION "+quoteIdent(pub.Publication)+" OWNER TO "+quoteIdent(pub.PublicationOwner)))
	b.WriteString("END\n")
	b.WriteString("$verself_postgres$;\n")
	return b.String()
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func sqlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quoteLiteral(value))
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]"
}

func qualifiedTableList(tables []string) string {
	qualified := make([]string, 0, len(tables))
	for _, table := range tables {
		qualified = append(qualified, "public."+quoteIdent(table))
	}
	return strings.Join(qualified, ", ")
}
