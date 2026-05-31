package controlplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ApplyConfig struct {
	BundlePath            string
	OpenBaoAddr           string
	OpenBaoCACert         string
	OpenBaoToken          string
	PostgresRuntime       string
	PostgresIdentPath     string
	PostgresReadyDeadline time.Duration
}

type ApplyResult struct {
	RuntimeSecrets int
	NomadRoles     int
	PostgresRoles  int
	Databases      int
	PeerMappings   int
	Publications   int
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

func ApplyBundleFile(ctx context.Context, cfg ApplyConfig) (ApplyResult, error) {
	cfg = cfg.withDefaults()
	body, err := os.ReadFile(cfg.BundlePath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read bundle: %w", err)
	}
	var bundle Bundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return ApplyResult{}, fmt.Errorf("decode bundle: %w", err)
	}
	if bundle.SchemaVersion != BundleSchemaVersion {
		return ApplyResult{}, fmt.Errorf("unsupported control-plane bundle schema %q", bundle.SchemaVersion)
	}
	bao, err := newBaoClient(cfg)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := bao.ready(ctx); err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{}
	if err := applyOpenBao(ctx, bao, bundle.OpenBao); err != nil {
		return result, err
	}
	result.RuntimeSecrets = len(bundle.OpenBao.RuntimeSecrets)
	result.NomadRoles = len(bundle.OpenBao.NomadRoles)
	pgResult, err := applyPostgres(ctx, cfg, bao, bundle.Postgres)
	if err != nil {
		return result, err
	}
	result.PostgresRoles = pgResult.PostgresRoles
	result.Databases = pgResult.Databases
	result.PeerMappings = pgResult.PeerMappings
	result.Publications = pgResult.Publications
	return result, nil
}

func (cfg ApplyConfig) withDefaults() ApplyConfig {
	if cfg.OpenBaoAddr == "" {
		cfg.OpenBaoAddr = envOr("BAO_ADDR", envOr("VAULT_ADDR", "https://127.0.0.1:8200"))
	}
	if cfg.OpenBaoCACert == "" {
		cfg.OpenBaoCACert = envOr("BAO_CACERT", "/etc/openbao/tls/cert.pem")
	}
	if cfg.OpenBaoToken == "" {
		cfg.OpenBaoToken = envOr("BAO_TOKEN", envOr("VAULT_TOKEN", ""))
	}
	if cfg.PostgresRuntime == "" {
		cfg.PostgresRuntime = envOr("VERSELF_POSTGRESQL_RUNTIME", filepath.Join(envOr("NOMAD_TASK_DIR", "local"), "postgresql", "opt", "verself", "postgresql"))
	}
	if cfg.PostgresIdentPath == "" {
		cfg.PostgresIdentPath = "/etc/postgresql/verself/pg_ident.conf"
	}
	if cfg.PostgresReadyDeadline == 0 {
		cfg.PostgresReadyDeadline = 60 * time.Second
	}
	return cfg
}

func newBaoClient(cfg ApplyConfig) (*baoClient, error) {
	if strings.TrimSpace(cfg.OpenBaoToken) == "" {
		return nil, fmt.Errorf("OpenBao token is required in BAO_TOKEN or VAULT_TOKEN")
	}
	roots := x509.NewCertPool()
	if cfg.OpenBaoCACert != "" {
		ca, err := os.ReadFile(cfg.OpenBaoCACert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
		}
		if !roots.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("parse OpenBao CA cert")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &baoClient{
		addr:  strings.TrimRight(cfg.OpenBaoAddr, "/"),
		token: strings.TrimSpace(cfg.OpenBaoToken),
		http:  &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}, nil
}

func applyOpenBao(ctx context.Context, c *baoClient, bundle OpenBaoBundle) error {
	for _, role := range bundle.NomadRoles {
		if err := c.writePolicy(ctx, role.Policy, runtimeReadPolicy(role.Secrets)); err != nil {
			return fmt.Errorf("write OpenBao policy %s: %w", role.Policy, err)
		}
		if err := c.writeNomadRole(ctx, role); err != nil {
			return fmt.Errorf("write OpenBao Nomad role %s: %w", role.Name, err)
		}
	}
	for _, secret := range bundle.RuntimeSecrets {
		if err := reconcileRuntimeSecret(ctx, c, secret); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRuntimeSecret(ctx context.Context, c *baoClient, secret RuntimeSecret) error {
	existing, found, err := c.readRuntimeSecret(ctx, secret.Name)
	if err != nil {
		return err
	}
	value := ""
	switch secret.Source.Kind {
	case RuntimeSecretSourceGenerated:
		if found && strings.TrimSpace(existing) != "" {
			return nil
		}
		generated, err := generateSecretValue(secret.Source.GeneratedBytes, secret.Source.Encoding)
		if err != nil {
			return fmt.Errorf("%s: generate value: %w", secret.Name, err)
		}
		value = generated
	case RuntimeSecretSourceSiteSecret:
		if found && strings.TrimSpace(existing) != "" {
			return nil
		}
		return fmt.Errorf("%s: site secret %s has not been imported into OpenBao", secret.Name, secret.Source.SiteSecretKey)
	case RuntimeSecretSourceFile:
		body, err := os.ReadFile(secret.Source.File)
		if err != nil {
			if found && strings.TrimSpace(existing) != "" {
				return nil
			}
			return fmt.Errorf("%s: read source file %s: %w", secret.Name, secret.Source.File, err)
		}
		value = strings.TrimSpace(string(body))
	default:
		return fmt.Errorf("%s: no runtime secret source", secret.Name)
	}
	if value == "" {
		return fmt.Errorf("%s: source value is empty", secret.Name)
	}
	if found && existing == value {
		return nil
	}
	if err := c.writeRuntimeSecret(ctx, secret.Name, value); err != nil {
		return err
	}
	fmt.Printf("substrate-control-plane: openbao runtime secret %s sha256=%s\n", secret.Name, fingerprint(value))
	return nil
}

func (c *baoClient) ready(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/v1/sys/health", http.NoBody)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			last = err
			time.Sleep(time.Second)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 473 {
			return nil
		}
		last = fmt.Errorf("OpenBao health status %d", resp.StatusCode)
		time.Sleep(time.Second)
	}
	return fmt.Errorf("OpenBao did not become ready: %w", last)
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

func (c *baoClient) writeRuntimeSecret(ctx context.Context, name, value string) error {
	return c.write(ctx, runtimeSecretAPIPath(name), map[string]any{"data": map[string]string{"value": value}})
}

func (c *baoClient) readRuntimeSecretRequired(ctx context.Context, name string) (string, error) {
	value, found, err := c.readRuntimeSecret(ctx, name)
	if err != nil {
		return "", err
	}
	if !found || value == "" {
		return "", fmt.Errorf("OpenBao runtime secret %s is empty or missing", name)
	}
	return value, nil
}

func (c *baoClient) writePolicy(ctx context.Context, name, policy string) error {
	return c.write(ctx, "v1/sys/policies/acl/"+url.PathEscape(name), map[string]string{"policy": policy})
}

func (c *baoClient) writeNomadRole(ctx context.Context, role NomadRole) error {
	return c.write(ctx, "v1/auth/"+NomadJWTMount+"/role/"+url.PathEscape(role.Name), map[string]any{
		"role_type":               "jwt",
		"bound_audiences":         []string{NomadAudience},
		"user_claim":              "/nomad_job_id",
		"user_claim_json_pointer": true,
		"claim_mappings": map[string]string{
			"nomad_namespace": "nomad_namespace",
			"nomad_job_id":    "nomad_job_id",
			"nomad_task":      "nomad_task",
		},
		"bound_claims": map[string]string{
			"nomad_job_id": role.JobID,
		},
		"token_type":             "service",
		"token_policies":         []string{role.Policy},
		"token_period":           "30m",
		"token_explicit_max_ttl": 0,
	})
}

func (c *baoClient) write(ctx context.Context, path string, body any) error {
	_, err := c.do(ctx, http.MethodPost, path, body, nil, http.StatusOK, http.StatusNoContent)
	return err
}

func (c *baoClient) do(ctx context.Context, method, path string, body any, out any, expected ...int) (int, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode openbao request %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.addr+"/"+path, reader)
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
	return "v1/" + RuntimeMount + "/data/secret/org/" + url.PathEscape(name)
}

func runtimeReadPolicy(secrets []string) string {
	var b strings.Builder
	for _, secret := range secrets {
		fmt.Fprintf(&b, "path %q {\n  capabilities = [\"read\"]\n}\n\n", RuntimeMount+"/data/secret/org/"+secret)
	}
	return b.String()
}

func applyPostgres(ctx context.Context, cfg ApplyConfig, bao *baoClient, plan PostgresBundle) (ApplyResult, error) {
	if err := waitForPostgres(ctx, cfg); err != nil {
		return ApplyResult{}, err
	}
	if err := writePeerMapFile(cfg.PostgresIdentPath, plan.PeerMappings); err != nil {
		return ApplyResult{}, err
	}
	roles := baseRoles(plan)
	if err := runPSQL(ctx, cfg, "postgres", baseSQL(roles, plan.Databases)); err != nil {
		return ApplyResult{}, err
	}
	secrets := map[string]string{}
	for _, role := range plan.ReplicationRoles {
		value, err := bao.readRuntimeSecretRequired(ctx, role.OpenBaoSecret)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("%s: %w", role.Path, err)
		}
		secrets[role.Name] = value
	}
	if len(plan.ReplicationRoles) > 0 {
		if err := runPSQL(ctx, cfg, "postgres", replicationRolesSQL(plan.ReplicationRoles, secrets)); err != nil {
			return ApplyResult{}, err
		}
	}
	for _, pub := range plan.Publications {
		if err := runPSQL(ctx, cfg, pub.Database, publicationSQL(pub)); err != nil {
			return ApplyResult{}, fmt.Errorf("%s/%s: %w", pub.Database, pub.Publication, err)
		}
	}
	return ApplyResult{
		PostgresRoles: len(roles),
		Databases:     len(plan.Databases),
		PeerMappings:  len(plan.PeerMappings),
		Publications:  len(plan.Publications),
	}, nil
}

func waitForPostgres(ctx context.Context, cfg ApplyConfig) error {
	deadline := time.Now().Add(cfg.PostgresReadyDeadline)
	var last error
	for time.Now().Before(deadline) {
		err := runPSQL(ctx, cfg, "postgres", "SELECT 1;\n")
		if err == nil {
			return nil
		}
		last = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("PostgreSQL did not become ready: %w", last)
}

func writePeerMapFile(path string, mappings []PostgresPeerMapping) error {
	pg, err := user.Lookup("postgres")
	if err != nil {
		return fmt.Errorf("lookup postgres user: %w", err)
	}
	uid, err := strconv.Atoi(pg.Uid)
	if err != nil {
		return fmt.Errorf("parse postgres uid: %w", err)
	}
	gid, err := strconv.Atoi(pg.Gid)
	if err != nil {
		return fmt.Errorf("parse postgres gid: %w", err)
	}
	if err := os.WriteFile(path, []byte(peerMap(mappings)), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}

func runPSQL(ctx context.Context, cfg ApplyConfig, database, sql string) error {
	psql := filepath.Join(cfg.PostgresRuntime, "usr", "lib", "postgresql", "16", "bin", "psql")
	cmd := exec.CommandContext(ctx, "/usr/sbin/runuser", "-u", "postgres", "--", psql, "-v", "ON_ERROR_STOP=1", "--no-psqlrc", "--quiet", "--dbname="+database, "--file=-")
	cmd.Stdin = strings.NewReader(sql)
	cmd.Env = append(os.Environ(),
		"LD_LIBRARY_PATH="+strings.Join([]string{
			filepath.Join(cfg.PostgresRuntime, "usr", "lib", "x86_64-linux-gnu"),
			filepath.Join(cfg.PostgresRuntime, "usr", "lib", "postgresql", "16", "lib"),
		}, ":"),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql %s: %w: %s", database, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func generateSecretValue(bytes int, encoding string) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	if encoding == "hex" {
		return hex.EncodeToString(raw), nil
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func envOr(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
