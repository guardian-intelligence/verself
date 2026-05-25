package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"

	opch "github.com/verself/operator-runtime/clickhouse"
	oppg "github.com/verself/operator-runtime/postgres"
	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	stalwartManagementRemoteAddr = "127.0.0.1:8090"
	stalwartAdminPasswordPath    = "/etc/credstore/stalwart/admin-password"
	testMailboxCredentialDir     = "test-mailboxes"
)

type mailTestAccountsOptions struct {
	operatorRuntimeOptions
	format         string
	pgUser         string
	pgRemotePort   int
	secretsFile    string
	resetSecrets   bool
	zitadelPATPath string
	zitadelAddr    string
}

type testMailAccountSpec struct {
	LocalPart string
}

type testMailboxCredential struct {
	Email          string `json:"email"`
	Principal      string `json:"stalwart_principal"`
	JMAPSessionURL string `json:"jmap_session_url"`
	Password       string `json:"password"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type stalwartPrincipal struct {
	ID          any      `json:"id,omitempty"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Secrets     []string `json:"secrets,omitempty"`
	Emails      []string `json:"emails"`
	Roles       []string `json:"roles"`
}

type stalwartStatusError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e stalwartStatusError) Error() string {
	return fmt.Sprintf("stalwart %s %s status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

type stalwartClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

func cmdMailTestAccounts(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mail test-accounts: missing subcommand (try `ensure`, `list`, or `delete`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ensure":
		return cmdMailTestAccountsEnsure(rest)
	case "list":
		return cmdMailTestAccountsList(rest)
	case "delete":
		return cmdMailTestAccountsDelete(rest)
	default:
		return fmt.Errorf("mail test-accounts: unknown subcommand: %s", sub)
	}
}

func cmdMailTestAccountsEnsure(args []string) error {
	opts := newMailTestAccountsOptions()
	fs := mailTestAccountsFlagSet("mail test-accounts ensure", opts)
	fs.BoolVar(&opts.resetSecrets, "reset-secrets", false, "rotate local mailbox passwords even when JMAP auth succeeds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("mail.test_accounts.ensure", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		client, err := newStalwartOperatorClient(rt)
		if err != nil {
			return err
		}
		rows := opruntime.Table{Headers: []string{"email", "status", "credential", "jmap"}}
		for _, email := range testMailAccountEmails(cfg.VerselfDomain) {
			status, credentialPath, jmapStatus, err := ensureTestMailbox(rt.Ctx, client, cfg.VerselfDomain, email, opts.resetSecrets)
			if err != nil {
				return err
			}
			rows.Rows = append(rows.Rows, []string{email, status, credentialPath, jmapStatus})
		}
		return printTableFormat(os.Stdout, rows, opts.format)
	})
}

func cmdMailTestAccountsList(args []string) error {
	opts := newMailTestAccountsOptions()
	fs := mailTestAccountsFlagSet("mail test-accounts list", opts)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("mail.test_accounts.list", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		cfg, err := loadMailConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		client, err := newStalwartOperatorClient(rt)
		if err != nil {
			return err
		}
		rows := opruntime.Table{Headers: []string{"email", "stalwart", "credential", "jmap", "credential_path"}}
		for _, email := range testMailAccountEmails(cfg.VerselfDomain) {
			_, found, err := client.GetPrincipal(rt.Ctx, email)
			if err != nil {
				return err
			}
			credPath := testMailboxCredentialPath(email)
			cred, credErr := readTestMailboxCredential(credPath)
			credentialStatus := "present"
			jmapStatus := "ok"
			if credErr != nil {
				credentialStatus = "missing"
				jmapStatus = "not_checked"
			} else if err := verifyJMAPCredential(rt.Ctx, cfg.VerselfDomain, cred); err != nil {
				jmapStatus = "failed"
			}
			stalwartStatus := "missing"
			if found {
				stalwartStatus = "present"
			}
			rows.Rows = append(rows.Rows, []string{email, stalwartStatus, credentialStatus, jmapStatus, credPath})
		}
		return printTableFormat(os.Stdout, rows, opts.format)
	})
}

func cmdMailTestAccountsDelete(args []string) error {
	opts := newMailTestAccountsOptions()
	fs := mailTestAccountsFlagSet("mail test-accounts delete", opts)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runOperatorRuntime("mail.test_accounts.delete", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		ctx, span := rt.Tracer.Start(rt.Ctx, "mail.test_accounts.delete")
		defer span.End()
		cfg, err := loadMailConfig(rt.RepoRoot, rt.Site)
		if err != nil {
			return err
		}
		emails := testMailAccountEmails(cfg.VerselfDomain)
		targets, err := discoverTestAccountCleanupTargets(ctx, rt, opts, emails)
		if err != nil {
			return err
		}
		span.SetAttributes(
			attribute.Int("mail.test_accounts.emails", len(emails)),
			attribute.Int("iam.signup_intents", len(targets.SignupIntentIDs)),
			attribute.Int("iam.orgs", len(targets.OrgIDs)),
			attribute.Int("zitadel.orgs", len(targets.ProviderOrgIDs)),
			attribute.Int("zitadel.users", len(targets.ProviderUserIDs)),
		)
		rows := opruntime.Table{Headers: []string{"surface", "rows_or_items"}}
		record := func(surface string, count int64) {
			rows.Rows = append(rows.Rows, []string{surface, fmt.Sprintf("%d", count)})
		}
		if n, err := cleanupNotificationsDB(ctx, rt, opts, emails, targets); err != nil {
			return err
		} else {
			record("notifications.postgres", n)
		}
		if n, err := cleanupEmailServiceDB(ctx, rt, opts, emails, targets); err != nil {
			return err
		} else {
			record("email_service.postgres", n)
		}
		if n, err := cleanupBillingDB(ctx, rt, opts, targets); err != nil {
			return err
		} else {
			record("billing.postgres", n)
		}
		if n, err := cleanupSpiceDB(ctx, rt, targets); err != nil {
			return err
		} else {
			record("spicedb.relationships", n)
		}
		if n, err := cleanupZitadel(ctx, rt, opts, targets); err != nil {
			return err
		} else {
			record("zitadel.api", n)
		}
		if n, err := cleanupIAMDB(ctx, rt, opts, emails, targets); err != nil {
			return err
		} else {
			record("iam_service.postgres", n)
		}
		if n, err := cleanupStalwart(ctx, rt, emails); err != nil {
			return err
		} else {
			record("stalwart.principals", n)
		}
		record("local.credentials", cleanupLocalTestMailboxCredentials(emails))
		return printTableFormat(os.Stdout, rows, opts.format)
	})
}

func newMailTestAccountsOptions() *mailTestAccountsOptions {
	opts := &mailTestAccountsOptions{
		format:         "table",
		pgUser:         envOr("PG_USER", oppg.DefaultUser),
		pgRemotePort:   envIntOr("PG_PORT", oppg.DefaultPort),
		secretsFile:    os.Getenv("SOPS_SECRETS_FILE"),
		zitadelPATPath: platformDefaultZitadelAdminPATPath,
		zitadelAddr:    platformDefaultZitadelRemote,
	}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	return opts
}

func mailTestAccountsFlagSet(name string, opts *mailTestAccountsOptions) *flag.FlagSet {
	fs := flagSet(name)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.format, "format", opts.format, "Output format: table, json, csv, or tsv")
	fs.StringVar(&opts.pgUser, "pg-user", opts.pgUser, "PostgreSQL user")
	fs.IntVar(&opts.pgRemotePort, "pg-remote-port", opts.pgRemotePort, "Remote PostgreSQL port on the worker loopback")
	fs.StringVar(&opts.secretsFile, "secrets-file", opts.secretsFile, "SOPS secrets file")
	fs.StringVar(&opts.zitadelPATPath, "zitadel-admin-pat-path", opts.zitadelPATPath, "Remote Zitadel admin PAT path")
	fs.StringVar(&opts.zitadelAddr, "zitadel-remote-addr", opts.zitadelAddr, "Remote Zitadel HTTP address reached over SSH")
	return fs
}

func testMailAccountSpecs() []testMailAccountSpec {
	return []testMailAccountSpec{
		{LocalPart: "signup-e2e"},
		{LocalPart: "signup+agent"},
		{LocalPart: "signup.dotted-name"},
		{LocalPart: "signup_equals=tag"},
		{LocalPart: "signup!bang"},
		{LocalPart: "signup~tilde"},
		{LocalPart: "signup%percent"},
		{LocalPart: "signup'quote"},
		{LocalPart: "signup&and"},
		{LocalPart: "signup?question"},
	}
}

func testMailAccountEmails(domain string) []string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	emails := make([]string, 0, len(testMailAccountSpecs()))
	for _, spec := range testMailAccountSpecs() {
		emails = append(emails, strings.ToLower(strings.TrimSpace(spec.LocalPart+"@"+domain)))
	}
	sort.Strings(emails)
	return emails
}

func newStalwartOperatorClient(rt *opruntime.Runtime) (stalwartClient, error) {
	rawPassword, err := opruntime.ReadRemoteFile(rt.Ctx, rt.SSH, stalwartAdminPasswordPath)
	if err != nil {
		return stalwartClient{}, fmt.Errorf("stalwart: read admin password: %w", err)
	}
	password := strings.TrimSpace(string(rawPassword))
	if password == "" {
		return stalwartClient{}, errors.New("stalwart: admin password is empty")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return rt.SSH.DialContext(ctx, "tcp", stalwartManagementRemoteAddr)
		},
		DisableKeepAlives: true,
	}
	return stalwartClient{
		baseURL:  "http://" + stalwartManagementRemoteAddr,
		username: "admin",
		password: password,
		client:   &http.Client{Transport: transport, Timeout: 10 * time.Second},
	}, nil
}

func ensureTestMailbox(ctx context.Context, client stalwartClient, domain, email string, resetSecret bool) (string, string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", "", "", errors.New("test mailbox email is empty")
	}
	principal := testMailboxDeliveryPrincipal(email)
	credentialPath := testMailboxCredentialPath(email)
	credential, credentialErr := readTestMailboxCredential(credentialPath)
	_, found, err := client.GetPrincipal(ctx, principal)
	if err != nil {
		return "", "", "", err
	}
	status := "unchanged"
	needsSecret := resetSecret || credentialErr != nil || !strings.EqualFold(credential.Principal, principal)
	if found && !needsSecret {
		if err := verifyJMAPCredential(ctx, domain, credential); err != nil {
			needsSecret = true
		}
	}
	if !found || needsSecret {
		password, err := randomCredentialSecret()
		if err != nil {
			return "", "", "", err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if !found {
			if err := client.CreatePrincipal(ctx, testStalwartPrincipal(principal, password)); err != nil {
				return "", "", "", err
			}
			if err := client.SetPrincipalPassword(ctx, principal, password); err != nil {
				return "", "", "", err
			}
			status = "created"
		} else {
			if err := client.SetPrincipalPassword(ctx, principal, password); err != nil {
				return "", "", "", err
			}
			status = "rotated_secret"
		}
		credential = testMailboxCredential{
			Email:          email,
			Principal:      principal,
			JMAPSessionURL: "https://mail." + strings.TrimSpace(domain) + "/jmap/session",
			Password:       password,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if existing, err := readTestMailboxCredential(credentialPath); err == nil && strings.TrimSpace(existing.CreatedAt) != "" {
			credential.CreatedAt = existing.CreatedAt
		}
		if err := writeTestMailboxCredential(credentialPath, credential); err != nil {
			return "", "", "", err
		}
	}
	if strings.TrimSpace(credential.JMAPSessionURL) == "" {
		credential.JMAPSessionURL = "https://mail." + strings.TrimSpace(domain) + "/jmap/session"
	}
	jmapStatus := "ok"
	if err := verifyJMAPCredentialWithRetry(ctx, domain, credential); err != nil {
		jmapStatus = "failed"
		return status, credentialPath, jmapStatus, err
	}
	return status, credentialPath, jmapStatus, nil
}

func testMailboxDeliveryPrincipal(email string) string {
	local, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok {
		return strings.ToLower(strings.TrimSpace(email))
	}
	if base, _, found := strings.Cut(local, "+"); found && strings.TrimSpace(base) != "" {
		local = base
	}
	return local + "@" + domain
}

func testStalwartPrincipal(principal, password string) stalwartPrincipal {
	return stalwartPrincipal{
		Type:        "individual",
		Name:        principal,
		Description: "Standalone signup E2E mailbox; not backed by a Verself/Zitadel user",
		Secrets:     []string{password},
		Emails:      []string{principal},
		Roles:       []string{"user"},
	}
}

func (c stalwartClient) GetPrincipal(ctx context.Context, principal string) (stalwartPrincipal, bool, error) {
	var out struct {
		Data  stalwartPrincipal `json:"data"`
		Error any               `json:"error"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/principal/"+url.PathEscape(strings.TrimSpace(principal)), nil, &out); err != nil {
		return stalwartPrincipal{}, false, err
	}
	if out.Error != nil || strings.TrimSpace(out.Data.Name) == "" {
		return stalwartPrincipal{}, false, nil
	}
	return out.Data, true, nil
}

func (c stalwartClient) CreatePrincipal(ctx context.Context, principal stalwartPrincipal) error {
	body := map[string]any{
		"type":        principal.Type,
		"name":        principal.Name,
		"description": principal.Description,
		"secrets":     principal.Secrets,
		"emails":      principal.Emails,
		"roles":       principal.Roles,
	}
	return c.doJSON(ctx, http.MethodPost, "/api/principal", body, nil)
}

func (c stalwartClient) SetPrincipalPassword(ctx context.Context, principal, password string) error {
	body := []map[string]any{{
		"action": "set",
		"field":  "secrets",
		"value":  password,
	}}
	return c.doJSON(ctx, http.MethodPatch, "/api/principal/"+url.PathEscape(strings.TrimSpace(principal)), body, nil)
}

func (c stalwartClient) DeletePrincipal(ctx context.Context, principal string) (bool, error) {
	_, found, err := c.GetPrincipal(ctx, principal)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if err := c.doJSON(ctx, http.MethodDelete, "/api/principal/"+url.PathEscape(strings.TrimSpace(principal)), nil, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (c stalwartClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("stalwart %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return stalwartStatusError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("stalwart %s %s decode response: %w", method, path, err)
	}
	return nil
}

func verifyJMAPCredential(ctx context.Context, domain string, credential testMailboxCredential) error {
	sessionURL := strings.TrimSpace(credential.JMAPSessionURL)
	if sessionURL == "" {
		sessionURL = "https://mail." + strings.TrimSpace(domain) + "/jmap/session"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sessionURL, nil)
	if err != nil {
		return err
	}
	username := strings.TrimSpace(credential.Principal)
	if username == "" {
		username = strings.TrimSpace(credential.Email)
	}
	req.SetBasicAuth(username, credential.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("jmap session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("jmap session status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decode jmap session: %w", err)
	}
	if _, ok := out.Capabilities["urn:ietf:params:jmap:mail"]; !ok {
		return errors.New("jmap session missing mail capability")
	}
	return nil
}

func verifyJMAPCredentialWithRetry(ctx context.Context, domain string, credential testMailboxCredential) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := verifyJMAPCredential(ctx, domain, credential); err != nil {
			lastErr = err
		} else {
			return nil
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func testMailboxCredentialPath(email string) string {
	root, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(root) == "" {
		root = filepath.Join(os.Getenv("HOME"), ".config")
	}
	name := base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(strings.TrimSpace(email)))) + ".json"
	return filepath.Join(root, "verself", testMailboxCredentialDir, name)
}

func readTestMailboxCredential(path string) (testMailboxCredential, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return testMailboxCredential{}, err
	}
	var credential testMailboxCredential
	if err := json.Unmarshal(raw, &credential); err != nil {
		return testMailboxCredential{}, err
	}
	if strings.TrimSpace(credential.Email) == "" || strings.TrimSpace(credential.Password) == "" {
		return testMailboxCredential{}, errors.New("credential is incomplete")
	}
	return credential, nil
}

func writeTestMailboxCredential(path string, credential testMailboxCredential) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(credential, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credential-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func cleanupLocalTestMailboxCredentials(emails []string) int64 {
	var count int64
	for _, email := range emails {
		if err := os.Remove(testMailboxCredentialPath(email)); err == nil {
			count++
		}
	}
	return count
}

func randomCredentialSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type testAccountCleanupTargets struct {
	SignupIntentIDs []string
	OrgIDs          []string
	ProviderOrgIDs  []string
	ProviderUserIDs []string
}

func discoverTestAccountCleanupTargets(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, emails []string) (testAccountCleanupTargets, error) {
	iam, err := openMailOperatorPG(ctx, rt, opts, "iam_service")
	if err != nil {
		return testAccountCleanupTargets{}, err
	}
	defer func() { _ = iam.Close(context.Background()) }()
	targets := testAccountCleanupTargets{}
	rows, err := iam.Query(ctx, `
SELECT signup_intent_id, org_id, identity_provider_org_id, identity_provider_user_id
FROM iam_signup_intents
WHERE lower(email) = ANY($1::text[])`, emails)
	if err != nil {
		return testAccountCleanupTargets{}, fmt.Errorf("query signup cleanup targets: %w", err)
	}
	for rows.Next() {
		var signupIntentID, orgID, providerOrgID, providerUserID string
		if err := rows.Scan(&signupIntentID, &orgID, &providerOrgID, &providerUserID); err != nil {
			rows.Close()
			return testAccountCleanupTargets{}, err
		}
		targets.SignupIntentIDs = append(targets.SignupIntentIDs, signupIntentID)
		targets.OrgIDs = append(targets.OrgIDs, orgID)
		targets.ProviderOrgIDs = append(targets.ProviderOrgIDs, providerOrgID)
		targets.ProviderUserIDs = append(targets.ProviderUserIDs, providerUserID)
	}
	if err := rows.Err(); err != nil {
		return testAccountCleanupTargets{}, err
	}
	rows.Close()
	accountRows, err := iam.Query(ctx, `
SELECT DISTINCT subject, coalesce(selected_org_id, '')
FROM iam_browser_accounts
WHERE lower(coalesce(email, '')) = ANY($1::text[])`, emails)
	if err != nil {
		return testAccountCleanupTargets{}, fmt.Errorf("query browser account cleanup targets: %w", err)
	}
	for accountRows.Next() {
		var subject, orgID string
		if err := accountRows.Scan(&subject, &orgID); err != nil {
			accountRows.Close()
			return testAccountCleanupTargets{}, err
		}
		targets.ProviderUserIDs = append(targets.ProviderUserIDs, subject)
		targets.OrgIDs = append(targets.OrgIDs, orgID)
	}
	if err := accountRows.Err(); err != nil {
		return testAccountCleanupTargets{}, err
	}
	accountRows.Close()
	zitadel, closeZitadel, err := newMailZitadelClient(ctx, rt, opts)
	if err != nil {
		return testAccountCleanupTargets{}, err
	}
	defer closeZitadel()
	for _, email := range emails {
		user, found, err := zitadel.FindHumanByEmail(ctx, email)
		if err != nil {
			return testAccountCleanupTargets{}, fmt.Errorf("zitadel find %s: %w", email, err)
		}
		if !found {
			continue
		}
		targets.ProviderUserIDs = append(targets.ProviderUserIDs, user.ID)
		targets.ProviderOrgIDs = append(targets.ProviderOrgIDs, user.ResourceOwner)
	}
	targets.SignupIntentIDs = compactNonEmpty(targets.SignupIntentIDs)
	targets.OrgIDs = compactNonEmpty(targets.OrgIDs)
	targets.ProviderOrgIDs = compactNonEmpty(targets.ProviderOrgIDs)
	targets.ProviderUserIDs = compactNonEmpty(targets.ProviderUserIDs)
	return targets, nil
}

func cleanupNotificationsDB(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, emails []string, targets testAccountCleanupTargets) (int64, error) {
	conn, err := openMailOperatorPG(ctx, rt, opts, "notifications_service")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var total int64
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM river_job j USING notification_delivery_attempts a WHERE j.kind = 'notifications.delivery.email' AND j.args @> jsonb_build_object('delivery_attempt_id', a.delivery_attempt_id) AND (a.org_id = ANY($1::text[]) OR lower(a.recipient_address) = ANY($2::text[]))`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM river_job j USING notification_events e WHERE j.kind = 'notifications.event.fanout' AND j.args @> jsonb_build_object('event_id', e.event_id) AND (e.org_id = ANY($1::text[]) OR e.recipient_subject_id = ANY($2::text[]))`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM notification_workflow_runs WHERE org_id = ANY($1::text[]) OR workflow_run_id IN (SELECT workflow_run_id FROM notification_delivery_attempts WHERE lower(recipient_address) = ANY($2::text[]))`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM notification_delivery_attempts WHERE org_id = ANY($1::text[]) OR lower(recipient_address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM user_notifications WHERE org_id = ANY($1::text[]) OR recipient_subject_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM notification_events WHERE org_id = ANY($1::text[]) OR recipient_subject_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM notification_projection_queue WHERE org_id = ANY($1::text[]) OR recipient_subject_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM notification_inbox_state WHERE org_id = ANY($1::text[]) OR recipient_subject_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM notification_preferences WHERE subject_id = ANY($1::text[])`, []any{targets.ProviderUserIDs}},
	} {
		if err := addDeletedRows(&total, execDelete(ctx, conn, statement.query, statement.args...)); err != nil {
			return total, err
		}
	}
	return total, nil
}

func cleanupEmailServiceDB(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, emails []string, targets testAccountCleanupTargets) (int64, error) {
	conn, err := openMailOperatorPG(ctx, rt, opts, "email_service")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var total int64
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM outbound_messages WHERE org_id = ANY($1::text[]) OR lower(to_address) = ANY($2::text[]) OR lower(from_address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM campaign_recipients WHERE contact_id IN (SELECT contact_id FROM audience_contacts WHERE org_id = ANY($1::text[]) OR lower(email_address) = ANY($2::text[]))`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM campaigns WHERE org_id = ANY($1::text[]) OR lower(from_address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM audience_contacts WHERE org_id = ANY($1::text[]) OR lower(email_address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM suppression_entries WHERE org_id = ANY($1::text[]) OR lower(email_address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM forwarding_rules WHERE lower(source_address) = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM send_as_grants WHERE lower(address) = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM email_mailboxes WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM email_bodies WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM emails WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM threads WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM mailboxes WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM sync_state WHERE account_id = ANY($1::text[])`, []any{emails}},
		{`DELETE FROM email_addresses WHERE org_id = ANY($1::text[]) OR lower(address) = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
		{`DELETE FROM email_identities WHERE org_id = ANY($1::text[]) OR lower(primary_address) = ANY($2::text[]) OR jmap_account_id = ANY($2::text[])`, []any{targets.OrgIDs, emails}},
	} {
		if err := addDeletedRows(&total, execDelete(ctx, conn, statement.query, statement.args...)); err != nil {
			return total, err
		}
	}
	return total, nil
}

func cleanupBillingDB(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, targets testAccountCleanupTargets) (int64, error) {
	conn, err := openMailOperatorPG(ctx, rt, opts, "billing")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	result := execDelete(ctx, conn, `DELETE FROM orgs WHERE org_id = ANY($1::text[])`, targets.OrgIDs)
	return result.rows, result.err
}

func cleanupIAMDB(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, emails []string, targets testAccountCleanupTargets) (int64, error) {
	conn, err := openMailOperatorPG(ctx, rt, opts, "iam_service")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	var total int64
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM iam_browser_resource_tokens WHERE account_handle IN (SELECT account_handle FROM iam_browser_accounts WHERE lower(coalesce(email, '')) = ANY($1::text[]) OR subject = ANY($2::text[]))`, []any{emails, targets.ProviderUserIDs}},
		{`DELETE FROM iam_browser_account_observations WHERE lower(coalesce(subject, '')) = ANY($1::text[]) OR account_handle IN (SELECT account_handle FROM iam_browser_accounts WHERE lower(coalesce(email, '')) = ANY($2::text[]) OR subject = ANY($1::text[]))`, []any{targets.ProviderUserIDs, emails}},
		{`UPDATE iam_browser_clients SET active_account_handle = NULL WHERE active_account_handle IN (SELECT account_handle FROM iam_browser_accounts WHERE lower(coalesce(email, '')) = ANY($1::text[]) OR subject = ANY($2::text[]))`, []any{emails, targets.ProviderUserIDs}},
		{`DELETE FROM iam_browser_accounts WHERE lower(coalesce(email, '')) = ANY($1::text[]) OR subject = ANY($2::text[])`, []any{emails, targets.ProviderUserIDs}},
		{`DELETE FROM iam_browser_login_transactions WHERE lower(coalesce(required_email, '')) = ANY($1::text[]) OR coalesce(required_subject, '') = ANY($2::text[]) OR coalesce(required_org_id, '') = ANY($3::text[])`, []any{emails, targets.ProviderUserIDs, targets.OrgIDs}},
		{`DELETE FROM iam_member_invite_acceptance_tokens WHERE lower(email) = ANY($1::text[]) OR user_id = ANY($2::text[]) OR org_id = ANY($3::text[])`, []any{emails, targets.ProviderUserIDs, targets.OrgIDs}},
		{`DELETE FROM iam_service_accounts WHERE org_id = ANY($1::text[]) OR subject_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM iam_signup_intents WHERE lower(email) = ANY($1::text[]) OR org_id = ANY($2::text[]) OR identity_provider_user_id = ANY($3::text[])`, []any{emails, targets.OrgIDs, targets.ProviderUserIDs}},
		{`DELETE FROM iam_organizations WHERE org_id = ANY($1::text[]) OR identity_provider_org_id = ANY($2::text[])`, []any{targets.OrgIDs, targets.ProviderOrgIDs}},
	} {
		if err := addDeletedRows(&total, execDelete(ctx, conn, statement.query, statement.args...)); err != nil {
			return total, err
		}
	}
	return total, nil
}

func cleanupZitadel(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, targets testAccountCleanupTargets) (int64, error) {
	client, closeFn, err := newMailZitadelClient(ctx, rt, opts)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	var total int64
	for _, userID := range targets.ProviderUserIDs {
		deleted, err := client.DeleteUser(ctx, userID)
		if err != nil {
			return total, err
		}
		if deleted {
			total++
		}
	}
	for _, orgID := range targets.ProviderOrgIDs {
		deleted, err := client.DeleteOrganization(ctx, orgID)
		if err != nil {
			return total, err
		}
		if deleted {
			total++
		}
	}
	return total, nil
}

func cleanupSpiceDB(ctx context.Context, rt *opruntime.Runtime, targets testAccountCleanupTargets) (int64, error) {
	var total int64
	for _, orgID := range targets.OrgIDs {
		for _, role := range []string{"owner", "admin", "member", "execution_lister", "billing_viewer", "source_viewer", "secret_user"} {
			roleObject := "role:org_" + orgID + "_role_" + role
			orgRelation := role
			if role == "execution_lister" {
				orgRelation = "execution_lister"
			}
			for _, userID := range targets.ProviderUserIDs {
				if err := runZedRelationshipDelete(ctx, rt, roleObject, "member", "user:"+encodeZedOpaqueID(userID)); err == nil {
					total++
				}
			}
			if err := runZedRelationshipDelete(ctx, rt, "org:"+orgID, orgRelation, roleObject+"#member"); err == nil {
				total++
			}
		}
	}
	return total, nil
}

func cleanupStalwart(ctx context.Context, rt *opruntime.Runtime, emails []string) (int64, error) {
	client, err := newStalwartOperatorClient(rt)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, principal := range testMailboxDeliveryPrincipals(emails) {
		deleted, err := client.DeletePrincipal(ctx, principal)
		if err != nil {
			return total, err
		}
		if deleted {
			total++
		}
	}
	return total, nil
}

func testMailboxDeliveryPrincipals(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	principals := make([]string, 0, len(emails))
	for _, email := range emails {
		principal := testMailboxDeliveryPrincipal(email)
		if principal == "" {
			continue
		}
		if _, ok := seen[principal]; ok {
			continue
		}
		seen[principal] = struct{}{}
		principals = append(principals, principal)
	}
	sort.Strings(principals)
	return principals
}

func newMailZitadelClient(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions) (platformZitadelClient, func(), error) {
	rawToken, err := opruntime.ReadRemoteFile(ctx, rt.SSH, opts.zitadelPATPath)
	if err != nil {
		return platformZitadelClient{}, func() {}, fmt.Errorf("zitadel: read admin PAT: %w", err)
	}
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return platformZitadelClient{}, func() {}, errors.New("zitadel: admin PAT is empty")
	}
	forward, err := rt.SSH.Forward(ctx, "zitadel-http", opts.zitadelAddr)
	if err != nil {
		return platformZitadelClient{}, func() {}, fmt.Errorf("zitadel: open HTTP forward: %w", err)
	}
	client := platformZitadelClient{
		BaseURL:    "http://" + forward.ListenAddr,
		HostHeader: "verself.sh",
		Token:      token,
		Client:     &http.Client{Timeout: 10 * time.Second},
	}
	return client, func() { _ = forward.Close() }, nil
}

func (c platformZitadelClient) DeleteUser(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	err := c.doJSON(ctx, http.MethodDelete, "/v2/users/"+url.PathEscape(userID), nil, nil, false)
	if err == nil {
		return true, nil
	}
	if isZitadelNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("delete Zitadel user %s: %w", userID, err)
}

func (c platformZitadelClient) DeleteOrganization(ctx context.Context, orgID string) (bool, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return false, nil
	}
	err := c.doJSON(ctx, http.MethodDelete, "/v2/organizations/"+url.PathEscape(orgID), nil, nil, false)
	if err == nil {
		return true, nil
	}
	if isZitadelNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("delete Zitadel organization %s: %w", orgID, err)
}

func isZitadelNotFound(err error) bool {
	var status platformZitadelStatusError
	return errors.As(err, &status) && status.Status == http.StatusNotFound
}

func openMailOperatorPG(ctx context.Context, rt *opruntime.Runtime, opts *mailTestAccountsOptions, dbName string) (*pgx.Conn, error) {
	passwordPath := opts.secretsFile
	if passwordPath == "" {
		passwordPath = opruntime.HostConfigurationSecretsPath(rt.RepoRoot, rt.Site)
	}
	return oppg.OpenOverSSH(ctx, rt, oppg.Config{
		Database:     dbName,
		User:         opts.pgUser,
		RemotePort:   opts.pgRemotePort,
		PasswordPath: passwordPath,
	})
}

type deleteResult struct {
	rows int64
	err  error
}

func execDelete(ctx context.Context, conn *pgx.Conn, query string, args ...any) deleteResult {
	execCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tag, err := conn.Exec(execCtx, query, args...)
	if err != nil {
		return deleteResult{err: err}
	}
	return deleteResult{rows: tag.RowsAffected()}
}

func addDeletedRows(total *int64, result deleteResult) error {
	if result.err != nil {
		return result.err
	}
	*total += result.rows
	return nil
}

func runZedRelationshipDelete(ctx context.Context, rt *opruntime.Runtime, resource, relation, subject string) error {
	zedCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	script := `token="$(sudo /bin/cat /etc/credstore/spicedb/grpc-preshared-key)"; exec /opt/verself/profile/bin/zed --endpoint 127.0.0.1:24009 --token "$token" --insecure --skip-version-check --max-retries 0 relationship delete "$@"`
	return rt.SSH.RunArgv(zedCtx, "", []string{"/bin/bash", "-lc", script, "_", resource, relation, subject}, nil, io.Discard, io.Discard)
}

func encodeZedOpaqueID(value string) string {
	return "b64_" + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(value)))
}

func compactNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
