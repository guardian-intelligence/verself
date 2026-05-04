package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
	"gopkg.in/yaml.v3"
)

const (
	billingSetUserStateTarget = "//src/billing-service/cmd/billing-set-user-state:billing-set-user-state"
	billingSetUserStateBin    = "bazel-bin/src/billing-service/cmd/billing-set-user-state/billing-set-user-state_/billing-set-user-state"
)

type personaOptions struct {
	operatorRuntimeOptions
}

type personaDefinition struct {
	Name               string
	HumanEmail         string
	HumanPasswordPath  string
	MachineUsername    string
	MachineSecretPath  string
	MailboxAccount     string
	IncludePlatformOps bool
	TokenProjects      []string
}

type hostMainVars struct {
	VerselfDomain string `yaml:"verself_domain"`
}

func cmdPersona(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("persona: missing subcommand (try `assume` or `user-state`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "assume":
		return cmdPersonaAssume(rest)
	case "user-state":
		return cmdPersonaUserState(rest)
	default:
		return fmt.Errorf("persona: unknown subcommand: %s", sub)
	}
}

func cmdPersonaUserState(args []string) error {
	fs := flagSet("persona user-state")
	opts := &personaOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	email := fs.String("email", "", "User email")
	org := fs.String("org", "", "Org slug")
	orgID := fs.String("org-id", "", "Numeric org ID")
	orgName := fs.String("org-name", "", "Org display name")
	state := fs.String("state", "", "Billing state")
	planID := fs.String("plan-id", "", "Plan ID")
	productID := fs.String("product-id", billingProductDefault, "Billing product ID")
	balanceUnits := fs.String("balance-units", "", "Balance in product units")
	balanceCents := fs.String("balance-cents", "", "Balance in cents")
	businessNow := fs.String("business-now", "", "Business-time override")
	overagePolicy := fs.String("overage-policy", "", "Overage policy")
	trustTier := fs.String("trust-tier", "", "Trust tier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("persona user-state: --email is required")
	}
	if *org == "" && *orgID == "" {
		return errors.New("persona user-state: --org or --org-id is required")
	}
	if *balanceUnits != "" && *balanceCents != "" {
		return errors.New("persona user-state: set only one of --balance-units or --balance-cents")
	}
	return runOperatorRuntime("persona.user_state", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		remoteArgs := []string{
			"--pg-dsn", "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable",
			"--email", *email,
			"--product-id", *productID,
		}
		addStringFlag := func(name, value string) {
			if value != "" {
				remoteArgs = append(remoteArgs, name, value)
			}
		}
		addStringFlag("--org-id", *orgID)
		addStringFlag("--org", *org)
		addStringFlag("--org-name", *orgName)
		addStringFlag("--state", *state)
		addStringFlag("--plan-id", *planID)
		addStringFlag("--balance-units", *balanceUnits)
		addStringFlag("--balance-cents", *balanceCents)
		addStringFlag("--business-now", *businessNow)
		addStringFlag("--overage-policy", *overagePolicy)
		addStringFlag("--trust-tier", *trustTier)
		return runRemoteBazelExecutable(rt, billingSetUserStateTarget, billingSetUserStateBin, "verself-billing-set-user-state", "billing", remoteArgs)
	})
}

func cmdPersonaAssume(args []string) error {
	fs := flagSet("persona assume")
	opts := &personaOptions{}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	outputPath := fs.String("output", "", "Output env file path")
	printEnv := fs.Bool("print", false, "Print env vars to stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("persona assume: persona name is required")
	}
	name := fs.Arg(0)
	return runOperatorRuntime("persona.assume", opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		def, err := resolvePersona(rt.RepoRoot, name)
		if err != nil {
			return err
		}
		if *outputPath == "" && !*printEnv {
			*outputPath = filepath.Join(rt.RepoRoot, "smoke-artifacts", "personas", def.Name+".env")
		}
		return assumePersona(rt, def, *outputPath, *printEnv)
	})
}

func resolvePersona(repoRoot, name string) (personaDefinition, error) {
	domain, err := loadVerselfDomain(repoRoot)
	if err != nil {
		return personaDefinition{}, err
	}
	switch name {
	case "platform-admin":
		return personaDefinition{
			Name:               name,
			HumanEmail:         "agent@" + domain,
			HumanPasswordPath:  "/etc/credstore/seed-system/platform-agent-password",
			MachineUsername:    "assume-platform-admin",
			MachineSecretPath:  "/etc/credstore/seed-system/assume-platform-admin-client-secret",
			MailboxAccount:     "agents",
			IncludePlatformOps: true,
			TokenProjects:      []string{"sandbox-rental", "iam-service", "secrets-service", "mailbox-service", "forgejo"},
		}, nil
	case "acme-admin":
		return personaDefinition{
			Name:              name,
			HumanEmail:        "acme-admin@" + domain,
			HumanPasswordPath: "/etc/credstore/seed-system/acme-admin-password",
			MachineUsername:   "assume-acme-admin",
			MachineSecretPath: "/etc/credstore/seed-system/assume-acme-admin-client-secret",
			TokenProjects:     []string{"sandbox-rental", "iam-service", "secrets-service"},
		}, nil
	case "acme-member":
		return personaDefinition{
			Name:              name,
			HumanEmail:        "acme-user@" + domain,
			HumanPasswordPath: "/etc/credstore/seed-system/acme-user-password",
			MachineUsername:   "assume-acme-member",
			MachineSecretPath: "/etc/credstore/seed-system/assume-acme-member-client-secret",
			TokenProjects:     []string{"sandbox-rental", "iam-service", "secrets-service"},
		}, nil
	default:
		return personaDefinition{}, fmt.Errorf("persona must be one of platform-admin, acme-admin, acme-member")
	}
}

func loadVerselfDomain(repoRoot string) (string, error) {
	path := filepath.Join(repoRoot, "src", "host-configuration", "ansible", "group_vars", "all", "topology", "ops.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var vars hostMainVars
	if err := yaml.Unmarshal(raw, &vars); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if strings.TrimSpace(vars.VerselfDomain) == "" {
		return "", fmt.Errorf("%s did not define verself_domain", path)
	}
	return strings.TrimSpace(vars.VerselfDomain), nil
}

func assumePersona(rt *opruntime.Runtime, def personaDefinition, outputPath string, printEnv bool) error {
	domain, err := loadVerselfDomain(rt.RepoRoot)
	if err != nil {
		return err
	}
	authBaseURL := "https://auth." + domain
	adminPAT, err := readRemoteSecretString(rt, "/etc/zitadel/admin.pat")
	if err != nil {
		return err
	}
	humanPassword, err := readRemoteSecretString(rt, def.HumanPasswordPath)
	if err != nil {
		return err
	}
	machineSecret, err := readRemoteSecretString(rt, def.MachineSecretPath)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	projectIDs := map[string]string{}
	projectTokens := map[string]string{}
	for _, project := range def.TokenProjects {
		id, err := zitadelProjectID(rt.Ctx, client, authBaseURL, adminPAT, project)
		if err != nil {
			return err
		}
		token, err := zitadelProjectToken(rt.Ctx, client, authBaseURL, def.MachineUsername, machineSecret, id)
		if err != nil {
			return err
		}
		projectIDs[project] = id
		projectTokens[project] = token
	}
	env := personaEnv(def, domain, authBaseURL, humanPassword, projectIDs, projectTokens)
	rendered := renderEnv(env)
	if printEnv {
		fmt.Print(rendered)
		return nil
	}
	if outputPath == "" {
		return errors.New("persona assume: output path is required unless --print is set")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), filepath.Base(outputPath)+".")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	finalized := false
	defer func() {
		if !finalized {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(rendered); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}
	finalized = true
	fmt.Fprintf(os.Stderr, "persona env written: %s\n", outputPath)
	fmt.Fprintf(os.Stderr, "source %s\n", shellExportValue(outputPath))
	return printIdentityMetadata(rt, client, def.Name, projectTokens["iam-service"])
}

func readRemoteSecretString(rt *opruntime.Runtime, path string) (string, error) {
	raw, err := opruntime.ReadRemoteFile(rt.Ctx, rt.SSH, path)
	if err != nil {
		return "", err
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if value == "" {
		return "", fmt.Errorf("remote secret %s is empty", path)
	}
	return value, nil
}

func zitadelProjectID(ctx context.Context, client *http.Client, baseURL, adminPAT, name string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"queries": []map[string]any{{
			"nameQuery": map[string]string{
				"name":   name,
				"method": "TEXT_QUERY_METHOD_EQUALS",
			},
		}},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/management/v1/projects/_search", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+adminPAT)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search Zitadel project %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("search Zitadel project %s: HTTP %d: %s", name, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode Zitadel project search: %w", err)
	}
	if len(payload.Result) == 0 || payload.Result[0].ID == "" {
		return "", fmt.Errorf("Zitadel project not found: %s", name)
	}
	return payload.Result[0].ID, nil
}

func zitadelProjectToken(ctx context.Context, client *http.Client, baseURL, username, secret, projectID string) (string, error) {
	scope := "openid profile urn:zitadel:iam:user:resourceowner urn:zitadel:iam:org:projects:roles urn:zitadel:iam:org:project:id:" + projectID + ":aud"
	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	values.Set("scope", scope)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/oauth/v2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(username, secret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mint Zitadel token for project %s: %w", projectID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("mint Zitadel token for project %s: HTTP %d: %s", projectID, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode Zitadel token: %w", err)
	}
	if payload.AccessToken == "" {
		return "", errors.New("Zitadel token response did not include access_token")
	}
	return payload.AccessToken, nil
}

func personaEnv(def personaDefinition, domain, authBaseURL, humanPassword string, projectIDs, projectTokens map[string]string) map[string]string {
	env := map[string]string{
		"VERSELF_PERSONA":           def.Name,
		"VERSELF_DOMAIN":            domain,
		"ZITADEL_ISSUER_URL":        authBaseURL,
		"ZITADEL_MACHINE_CLIENT_ID": def.MachineUsername,
		"TEST_EMAIL":                def.HumanEmail,
		"TEST_PASSWORD":             humanPassword,
		"BROWSER_EMAIL":             def.HumanEmail,
		"BROWSER_PASSWORD":          humanPassword,
		"VERSELF_WEB_URL":           "https://" + domain,
		"WEBMAIL_URL":               "https://mail." + domain,
		"FORGEJO_URL":               "https://git." + domain,
	}
	addProjectEnv := func(project, prefix string) {
		if token := projectTokens[project]; token != "" {
			env[prefix+"_AUTH_AUDIENCE"] = projectIDs[project]
			env[prefix+"_ACCESS_TOKEN"] = token
			env[prefix+"_TOKEN"] = token
		}
	}
	addProjectEnv("sandbox-rental", "SANDBOX_RENTAL")
	addProjectEnv("iam-service", "IAM_SERVICE")
	if token := projectTokens["iam-service"]; token != "" {
		env["SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE"] = projectIDs["iam-service"]
		env["SOURCE_CODE_HOSTING_SERVICE_ACCESS_TOKEN"] = token
		env["SOURCE_CODE_HOSTING_SERVICE_TOKEN"] = token
	}
	addProjectEnv("secrets-service", "SECRETS_SERVICE")
	addProjectEnv("mailbox-service", "MAILBOX_SERVICE")
	if token := projectTokens["forgejo"]; token != "" {
		env["FORGEJO_AUTH_AUDIENCE"] = projectIDs["forgejo"]
		env["FORGEJO_OIDC_ACCESS_TOKEN"] = token
		env["FORGEJO_OIDC_TOKEN"] = token
	}
	if def.IncludePlatformOps {
		env["MAILBOX_ACCOUNT"] = def.MailboxAccount
		env["MAIL_OPERATOR_COMMAND"] = "aspect mail list --mailbox=" + def.MailboxAccount
		env["CLICKHOUSE_OPERATOR_COMMAND"] = "aspect db ch query --query='SELECT 1'"
		env["FORGEJO_OPERATOR_CREDENTIAL"] = "provider-native forgejo-automation token in /etc/credstore/forgejo/automation-token"
	}
	return env
}

func renderEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		out.WriteString("export ")
		out.WriteString(key)
		out.WriteByte('=')
		out.WriteString(shellExportValue(env[key]))
		out.WriteByte('\n')
	}
	return out.String()
}

func shellExportValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func printIdentityMetadata(rt *opruntime.Runtime, client *http.Client, personaName, token string) error {
	if token == "" {
		return errors.New("iam-service token is missing")
	}
	forward, err := rt.SSH.Forward(rt.Ctx, "iam-service", "127.0.0.1:4248")
	if err != nil {
		return err
	}
	defer func() { _ = forward.Close() }()
	baseURL := "http://" + forward.ListenAddr
	if err := waitForHTTP(rt.Ctx, client, baseURL+"/readyz", http.StatusOK); err != nil {
		return err
	}
	access, err := identityAPIGet(rt.Ctx, client, baseURL+"/api/v1/organization", token)
	if err != nil {
		return err
	}
	operations, err := identityAPIGet(rt.Ctx, client, baseURL+"/api/v1/organization/operations", token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: iam-service operation catalog endpoint unavailable; continuing with empty operations metadata")
		operations = map[string]any{"services": []any{}}
	}
	metadata := identityMetadata(personaName, access, operations)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(metadata)
}

func waitForHTTP(ctx context.Context, client *http.Client, endpoint string, expected int) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == expected {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not return %d", endpoint, expected)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func identityAPIGet(ctx context.Context, client *http.Client, endpoint, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("identity API %s: HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func identityMetadata(personaName string, access, operations map[string]any) map[string]any {
	permissions := stringSet(access["permissions"])
	operationPermissions := map[string]bool{}
	effectiveServices := []map[string]any{}
	if services, ok := operations["services"].([]any); ok {
		for _, rawService := range services {
			service, ok := rawService.(map[string]any)
			if !ok {
				continue
			}
			effectiveOps := []any{}
			if ops, ok := service["operations"].([]any); ok {
				for _, rawOp := range ops {
					op, ok := rawOp.(map[string]any)
					if !ok {
						continue
					}
					permission, _ := op["permission"].(string)
					if permission != "" {
						operationPermissions[permission] = true
					}
					if permissions[permission] {
						effectiveOps = append(effectiveOps, op)
					}
				}
			}
			if len(effectiveOps) > 0 {
				effectiveServices = append(effectiveServices, map[string]any{
					"service":    service["service"],
					"operations": effectiveOps,
				})
			}
		}
	}
	withoutDeclared := []string{}
	for permission := range permissions {
		if !operationPermissions[permission] {
			withoutDeclared = append(withoutDeclared, permission)
		}
	}
	sort.Strings(withoutDeclared)
	return map[string]any{
		"persona": personaName,
		"iam_service": map[string]any{
			"access":     access,
			"operations": operations,
			"effective_operations": map[string]any{
				"org_id":                                 access["org_id"],
				"caller":                                 access["caller"],
				"permissions":                            sortedSet(permissions),
				"services":                               effectiveServices,
				"permissions_without_declared_operation": withoutDeclared,
			},
		},
	}
}

func stringSet(value any) map[string]bool {
	out := map[string]bool{}
	values, ok := value.([]any)
	if !ok {
		return out
	}
	for _, raw := range values {
		if s, ok := raw.(string); ok && s != "" {
			out[s] = true
		}
	}
	return out
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
