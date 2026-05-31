package postgresruntime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	openBaoRemotePort = 8200
	openBaoCACert     = "/etc/openbao/tls/cert.pem"
	runtimeMount      = "kv-runtime"
)

type ApplyOptions struct {
	Site   string
	SSH    *sshtun.Client
	Tracer trace.Tracer
	Token  string
}

type Result struct {
	Roles            int
	Databases        int
	ReplicationRoles int
	PeerMappings     int
	Publications     int
}

type baoClient struct {
	addr  string
	token string
	http  *http.Client
}

type baoResponse struct {
	Data   map[string]any `json:"data"`
	Errors []string       `json:"errors"`
}

func ApplyBase(ctx context.Context, opts ApplyOptions, plan Plan) (Result, error) {
	ctx, span := startSpan(ctx, opts, "verself_deploy.postgres.base_reconcile")
	defer span.End()
	if err := validateOpts(opts); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	result := Result{Databases: len(plan.Databases), PeerMappings: len(plan.PeerMappings)}
	if err := writePeerMap(ctx, opts, plan.PeerMappings); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	roles := baseRoles(plan)
	sql := baseSQL(roles, plan.Databases)
	if err := runPSQL(ctx, opts, "postgres", sql); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	result.Roles = len(roles)
	span.SetAttributes(
		attribute.Int("verself.postgres.roles", result.Roles),
		attribute.Int("verself.postgres.databases", result.Databases),
		attribute.Int("verself.postgres.peer_mappings", result.PeerMappings),
	)
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func ApplyReplicationRoles(ctx context.Context, opts ApplyOptions, plan Plan) (Result, error) {
	ctx, span := startSpan(ctx, opts, "verself_deploy.postgres.replication_roles_reconcile")
	defer span.End()
	if err := validateOpts(opts); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	if len(plan.ReplicationRoles) == 0 {
		span.SetStatus(codes.Ok, "")
		return Result{}, nil
	}
	bao, closeBao, err := dialOpenBao(ctx, opts.SSH, opts.Token)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	defer closeBao()
	secrets := make(map[string]string, len(plan.ReplicationRoles))
	for _, role := range plan.ReplicationRoles {
		value, found, err := bao.readRuntimeSecret(ctx, role.OpenBaoSecret)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return Result{}, err
		}
		if !found || value == "" {
			err := fmt.Errorf("%s: OpenBao runtime secret %s is empty or missing", role.Path, role.OpenBaoSecret)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return Result{}, err
		}
		secrets[role.Name] = value
	}
	if err := runPSQL(ctx, opts, "postgres", replicationRolesSQL(plan.ReplicationRoles, secrets)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	result := Result{ReplicationRoles: len(plan.ReplicationRoles)}
	span.SetAttributes(attribute.Int("verself.postgres.replication_roles", result.ReplicationRoles))
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func ApplyPublications(ctx context.Context, opts ApplyOptions, plan Plan, strict bool) (Result, error) {
	ctx, span := startSpan(ctx, opts, "verself_deploy.postgres.publications_reconcile")
	defer span.End()
	span.SetAttributes(attribute.Bool("verself.postgres.publications.strict", strict))
	if err := validateOpts(opts); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Result{}, err
	}
	for _, pub := range plan.Publications {
		if err := runPSQL(ctx, opts, pub.Database, publicationSQL(pub, strict)); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return Result{}, err
		}
	}
	result := Result{Publications: len(plan.Publications)}
	span.SetAttributes(attribute.Int("verself.postgres.publications", result.Publications))
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func startSpan(ctx context.Context, opts ApplyOptions, name string) (context.Context, trace.Span) {
	return opts.Tracer.Start(ctx, name,
		trace.WithAttributes(
			attribute.String("verself.site", opts.Site),
		),
	)
}

func validateOpts(opts ApplyOptions) error {
	if opts.SSH == nil {
		return fmt.Errorf("ssh client is required")
	}
	if opts.Tracer == nil {
		return fmt.Errorf("tracer is required")
	}
	return nil
}

func writePeerMap(ctx context.Context, opts ApplyOptions, mappings []PeerMapping) error {
	var b strings.Builder
	b.WriteString("# PostgreSQL user name maps.\n# Managed by verself-deploy.\n")
	for _, mapping := range mappings {
		fmt.Fprintf(&b, "verself_services      %-24s %s\n", mapping.SystemUser, mapping.PGUser)
	}
	if err := opts.SSH.Run(ctx, "sudo /usr/bin/tee /etc/postgresql/verself/pg_ident.conf >/dev/null", strings.NewReader(b.String())); err != nil {
		return err
	}
	_, err := opts.SSH.Exec(ctx, "sudo /bin/chown postgres:postgres /etc/postgresql/verself/pg_ident.conf && sudo /bin/chmod 0600 /etc/postgresql/verself/pg_ident.conf")
	return err
}

func baseRoles(plan Plan) []string {
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

func baseSQL(roles []string, dbs []Database) string {
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

func replicationRolesSQL(roles []ReplicationRole, secrets map[string]string) string {
	var b strings.Builder
	b.WriteString("SET client_min_messages TO warning;\n")
	for _, role := range roles {
		fmt.Fprintf(&b, "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN CREATE ROLE %s LOGIN REPLICATION CONNECTION LIMIT %d; END IF; END $$;\n", quoteLiteral(role.Name), quoteIdent(role.Name), role.ConnectionLimit)
		fmt.Fprintf(&b, "ALTER ROLE %s WITH LOGIN REPLICATION CONNECTION LIMIT %d PASSWORD %s;\n", quoteIdent(role.Name), role.ConnectionLimit, quoteLiteral(secrets[role.Name]))
	}
	return b.String()
}

func publicationSQL(pub Publication, strict bool) string {
	var b strings.Builder
	tableArray := sqlStringArray(pub.Tables)
	tableList := qualifiedTableList(pub.Tables)
	onMissing := "RAISE NOTICE 'publication % skipped because tables are missing: %', " + quoteLiteral(pub.Publication) + ", missing; RETURN;"
	if strict {
		onMissing = "RAISE EXCEPTION 'publication % missing tables: %', " + quoteLiteral(pub.Publication) + ", missing;"
	}
	b.WriteString("SET client_min_messages TO warning;\n")
	b.WriteString("DO $verself_postgres$\n")
	b.WriteString("DECLARE missing text[];\n")
	b.WriteString("BEGIN\n")
	fmt.Fprintf(&b, "  SELECT array_agg(t) INTO missing FROM unnest(%s) AS t WHERE to_regclass('public.' || quote_ident(t)) IS NULL;\n", tableArray)
	b.WriteString("  IF COALESCE(array_length(missing, 1), 0) > 0 THEN\n")
	fmt.Fprintf(&b, "    %s\n", onMissing)
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

func runPSQL(ctx context.Context, opts ApplyOptions, database, sql string) error {
	script := `psql="$VERSELF_POSTGRESQL_RUNTIME/opt/verself/postgresql/usr/lib/postgresql/16/bin/psql"; exec "$psql" -v ON_ERROR_STOP=1 --no-psqlrc --quiet --dbname="$1" --file=-`
	cmd := strings.Join([]string{
		"alloc=$(sudo /opt/verself/profile/bin/nomad job status -json postgresql | jq -r '.[0].Allocations[] | select(.ClientStatus == \"running\" and .DesiredStatus == \"run\") | .ID' | head -1)",
		"test -n \"$alloc\"",
		"sudo /opt/verself/profile/bin/nomad alloc exec -i -task server \"$alloc\" /bin/sh -ec " + shellQuote(script) + " _ " + shellQuote(database),
	}, "\n")
	return opts.SSH.Run(ctx, cmd, strings.NewReader(sql))
}

func dialOpenBao(ctx context.Context, sshClient *sshtun.Client, token string) (*baoClient, func(), error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, func() {}, fmt.Errorf("VERSELF_OPENBAO_RECONCILE_TOKEN is required for OpenBao-backed PostgreSQL reconciliation")
	}
	forward, err := sshClient.Forward(ctx, "openbao-postgres", openBaoRemotePort)
	if err != nil {
		return nil, func() {}, err
	}
	closeForward := func() { _ = forward.Close() }
	caPEM, err := sudoCat(ctx, sshClient, openBaoCACert)
	if err != nil {
		closeForward()
		return nil, func() {}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		closeForward()
		return nil, func() {}, fmt.Errorf("parse OpenBao CA bundle")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &baoClient{
		addr:  "https://" + forward.ListenAddr,
		token: token,
		http:  &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}, closeForward, nil
}

func (c *baoClient) readRuntimeSecret(ctx context.Context, name string) (string, bool, error) {
	var response baoResponse
	status, err := c.do(ctx, http.MethodGet, runtimeSecretAPIPath(name), nil, &response, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	data, _ := response.Data["data"].(map[string]any)
	return strings.TrimSpace(fmt.Sprint(data["value"])), true, nil
}

func (c *baoClient) do(ctx context.Context, method, path string, body any, out any, expected ...int) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode openbao request %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.addr, "/")+"/"+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Vault-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	for _, code := range expected {
		if resp.StatusCode == code {
			if out != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return resp.StatusCode, fmt.Errorf("decode openbao %s %s: %w", method, path, err)
				}
			}
			return resp.StatusCode, nil
		}
	}
	return resp.StatusCode, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func runtimeSecretAPIPath(name string) string {
	return "v1/" + runtimeMount + "/data/secret/org/" + url.PathEscape(name)
}

func sudoCat(ctx context.Context, sshClient *sshtun.Client, path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return sshClient.Exec(ctx, "sudo /bin/cat -- "+strconv.Quote(path))
}

func sqlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quoteLiteral(value))
	}
	return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]"
}

func qualifiedTableList(tables []string) string {
	quoted := make([]string, 0, len(tables))
	for _, table := range tables {
		quoted = append(quoted, "public."+quoteIdent(table))
	}
	return strings.Join(quoted, ", ")
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
