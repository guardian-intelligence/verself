package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/iam-service/internal/api"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
	"github.com/verself/iam-service/internal/zitadel"
	"github.com/verself/iam-service/migrations"
	iamschema "github.com/verself/iam-service/schema"
	pwnedpasswords "github.com/verself/integrations/hibp/pwned-passwords"
	notificationsinternalclient "github.com/verself/notifications-service/internalclient"
	verselfotel "github.com/verself/observability/otel"
	auth "github.com/verself/service-runtime/auth"
	"github.com/verself/service-runtime/envconfig"
	"github.com/verself/service-runtime/httpserver"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	serviceName                        = "iam-service"
	serviceVersion                     = "1.0.1"
	defaultPwnedPasswordsRangeEndpoint = "https://api.pwnedpasswords.com"
	requestBodyLimit                   = 1 << 20
)

func main() {
	if handled, err := runIAMRecoveryCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runBootstrapPolicyCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if handled, err := runMigrationCLI(context.Background(), os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigrationCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "migrate" {
		return false, nil
	}
	opts, remaining, err := parseIAMMigrationOptions(args[1:])
	if err != nil {
		return true, err
	}
	if len(remaining) != 1 || remaining[0] != "up" {
		return true, errors.New("usage: migrate [--resource-graph PATH] [--resource-name NAME] up")
	}
	runtimeCfg, err := loadIAMRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return true, err
	}
	return true, migrations.UpDSN(ctx, serviceName, runtimeCfg.PostgresDSN)
}

func runBootstrapPolicyCLI(ctx context.Context, args []string) (bool, error) {
	if len(args) < 1 || args[0] != "bootstrap-policy" {
		return false, nil
	}
	fs := flag.NewFlagSet("bootstrap-policy", flag.ExitOnError)
	spiceDBEndpoint := fs.String("spicedb-endpoint", "", "SpiceDB gRPC endpoint")
	spiceDBPresharedKeyFile := fs.String("spicedb-preshared-key-file", "", "SpiceDB preshared key file")
	orgID := fs.String("org-id", "", "Verself public organization ID")
	ownerSubject := fs.String("owner-subject", "", "Zitadel subject ID for a human organization owner")
	if err := fs.Parse(args[1:]); err != nil {
		return true, err
	}
	if strings.TrimSpace(*spiceDBEndpoint) == "" {
		return true, fmt.Errorf("bootstrap-policy: --spicedb-endpoint is required")
	}
	if strings.TrimSpace(*spiceDBPresharedKeyFile) == "" {
		return true, fmt.Errorf("bootstrap-policy: --spicedb-preshared-key-file is required")
	}
	if strings.TrimSpace(*orgID) == "" {
		return true, fmt.Errorf("bootstrap-policy: --org-id is required")
	}
	if strings.TrimSpace(*ownerSubject) == "" {
		return true, fmt.Errorf("bootstrap-policy: --owner-subject is required")
	}
	keyRaw, err := os.ReadFile(*spiceDBPresharedKeyFile)
	if err != nil {
		return true, fmt.Errorf("bootstrap-policy: read spicedb preshared key: %w", err)
	}
	spice, err := spicedb.New(ctx, spicedb.Config{
		Endpoint:     strings.TrimSpace(*spiceDBEndpoint),
		PresharedKey: strings.TrimSpace(string(keyRaw)),
	})
	if err != nil {
		return true, err
	}
	defer func() { _ = spice.Close() }()
	schemaCtx, schemaCancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = spice.WriteSchema(schemaCtx, iamschema.Verself)
	schemaCancel()
	if err != nil {
		return true, fmt.Errorf("bootstrap-policy: write spicedb schema: %w", err)
	}
	authzService := authz.New(spice)
	current, err := authzService.GetOrganizationPolicy(ctx, strings.TrimSpace(*orgID))
	if err != nil {
		return true, err
	}
	ownerMember := "user:" + strings.TrimSpace(*ownerSubject)
	if policyHasMember(current, "roles/owner", ownerMember) {
		return true, nil
	}
	next := current
	next.Bindings = appendOwnerBinding(next.Bindings, ownerMember)
	_, err = authzService.SetOrganizationPolicy(ctx, strings.TrimSpace(*orgID), next, "iam.platform_bootstrap_policy")
	if err != nil {
		return true, err
	}
	return true, nil
}

func policyHasMember(policy authz.Policy, role, member string) bool {
	for _, binding := range policy.Bindings {
		if binding.Role != role {
			continue
		}
		for _, existing := range binding.Members {
			if existing == member {
				return true
			}
		}
	}
	return false
}

func appendOwnerBinding(bindings []authz.PolicyBinding, ownerMember string) []authz.PolicyBinding {
	out := append([]authz.PolicyBinding(nil), bindings...)
	for i := range out {
		if out[i].Role != "roles/owner" {
			continue
		}
		out[i].Members = append(append([]string(nil), out[i].Members...), ownerMember)
		return out
	}
	return append(out, authz.PolicyBinding{Role: "roles/owner", Members: []string{ownerMember}})
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	opts, err := parseIAMRunOptions(args)
	if err != nil {
		return err
	}
	runtimeCfg, err := loadIAMRuntimeConfig(opts.ResourceGraph, opts.ResourceName)
	if err != nil {
		return err
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: serviceName, ServiceVersion: serviceVersion})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.ErrorContext(context.Background(), "iam-service otel shutdown", "error", err)
		}
	}()

	cfg := envconfig.New()
	zitadelActionSigningKey := cfg.RequireCredential(runtimeCfg.ZitadelActionSigningKeyName)
	browserOIDCClientID := cfg.RequireCredential(runtimeCfg.BrowserOIDCClientIDName)
	browserOIDCClientSecret := cfg.RequireCredential(runtimeCfg.BrowserOIDCClientSecretName)
	githubLoginIDPID := ""
	if runtimeCfg.GithubLoginIDPName != "" {
		githubLoginIDPID = cfg.RequireCredential(runtimeCfg.GithubLoginIDPName)
	}
	authAudience := cfg.RequireCredential(runtimeCfg.AuthAudienceName)
	zitadelBaseURL := cfg.RequireURL("IAM_ZITADEL_BASE_URL")
	spiceDBEndpoint := cfg.RequireString("IAM_SPICEDB_GRPC_ENDPOINT")
	zitadelAdminToken := cfg.RequireCredential(runtimeCfg.ZitadelAdminTokenName)
	spiceDBPresharedKey := cfg.RequireCredential(runtimeCfg.SpiceDBPresharedKeyName)
	emailIdentityHMACKey := cfg.RequireCredential(runtimeCfg.EmailIdentityHMACKeyName)
	if err := cfg.Err(); err != nil {
		return err
	}
	if len(emailIdentityHMACKey) < 32 {
		return fmt.Errorf("iam email identity secret must be at least 32 bytes")
	}

	pg, err := openPool(ctx, runtimeCfg.PostgresDSN, runtimeCfg.PostgresMaxConns)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pg.Close()

	spiffeSource, err := workloadauth.Source(ctx, runtimeCfg.SPIFFEEndpointSocket)
	if err != nil {
		return fmt.Errorf("iam spiffe workload source: %w", err)
	}
	defer func() {
		if err := spiffeSource.Close(); err != nil {
			logger.ErrorContext(context.Background(), "iam-service spiffe source close", "error", err)
		}
	}()

	zitadelClient, err := zitadel.New(zitadel.Config{
		BaseURL:    zitadelBaseURL,
		HostHeader: runtimeCfg.ZitadelHost,
		AdminToken: zitadelAdminToken,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
	})
	if err != nil {
		return err
	}
	pwnedPasswordsClient, err := pwnedpasswords.New(pwnedpasswords.Config{
		RangeEndpoint: runtimeCfg.PwnedPasswordsRangeEndpoint,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
		UserAgent: serviceName + "/" + serviceVersion,
	})
	if err != nil {
		return fmt.Errorf("iam pwned passwords client: %w", err)
	}
	passwordChecker := pwnedPasswordChecker{client: pwnedPasswordsClient}
	chTLSConfig, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, runtimeCfg.ClickHouseCACertPath)
	if err != nil {
		return fmt.Errorf("iam clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{runtimeCfg.ClickHouseAddress},
		Auth: clickhouse.Auth{
			Database: "verself",
			Username: runtimeCfg.ClickHouseUser,
		},
		TLS: chTLSConfig,
	})
	if err != nil {
		return fmt.Errorf("open iam clickhouse: %w", err)
	}
	defer func() { _ = chConn.Close() }()
	chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer chPingCancel()
	if err := chConn.Ping(chPingCtx); err != nil {
		return fmt.Errorf("ping iam clickhouse: %w", err)
	}
	spice, err := spicedb.New(ctx, spicedb.Config{
		Endpoint:     spiceDBEndpoint,
		PresharedKey: spiceDBPresharedKey,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := spice.Close(); err != nil {
			logger.ErrorContext(context.Background(), "iam-service spicedb client close", "error", err)
		}
	}()
	schemaCtx, schemaCancel := context.WithTimeout(ctx, 2*time.Second)
	schemaToken, err := spice.WriteSchema(schemaCtx, iamschema.Verself)
	schemaCancel()
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "iam-service spicedb schema reconciled", "zed_token", schemaToken)
	authzService := authz.New(spice)
	billingHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceBilling, nil)
	if err != nil {
		return fmt.Errorf("iam billing mtls: %w", err)
	}
	billingClient, err := billingclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceBilling), billingclient.WithHTTPClient(billingHTTPClient))
	if err != nil {
		return fmt.Errorf("iam billing client: %w", err)
	}
	store := identity.SQLStore{PG: pg, CH: chConn}
	identityService := &identity.Service{
		Store:              store,
		Directory:          zitadelClient,
		AuthorizationGraph: authzService,
		PolicyWriter:       organizationOwnerPolicyWriter{authz: authzService},
		Billing:            billingOrganizationProvisioner{client: billingClient},
		PasswordChecker:    passwordChecker,
		ProjectID:          authAudience,
		IdentityIssuer:     runtimeCfg.AuthIssuerURL,
		EmailIdentityKey:   []byte(emailIdentityHMACKey),
	}
	api.ConfigureAPIActivitySink(workloadauth.InternalURL(workloadauth.ServiceGovernance), spiffeSource)
	notificationsHTTPClient, err := workloadauth.MTLSClientForService(spiffeSource, workloadauth.ServiceNotifications, nil)
	if err != nil {
		return fmt.Errorf("iam notifications mtls: %w", err)
	}
	notificationsClient, err := notificationsinternalclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceNotifications), notificationsinternalclient.WithHTTPClient(notificationsHTTPClient))
	if err != nil {
		return fmt.Errorf("iam notifications client: %w", err)
	}
	inviteNotifier := notificationInviteSender{client: notificationsClient}
	signupNotifier := notificationSignupSender{client: notificationsClient}
	browserAuth, err := api.NewBrowserAuth(ctx, api.BrowserAuthConfig{
		PG:                 pg,
		Logger:             logger,
		IssuerURL:          runtimeCfg.AuthIssuerURL,
		ClientID:           browserOIDCClientID,
		ClientSecret:       browserOIDCClientSecret,
		PublicBaseURL:      runtimeCfg.BrowserAuthPublicBaseURL,
		ProductAudience:    authAudience,
		Authz:              authzService,
		ProviderSession:    zitadelClient,
		ProviderLogin:      zitadelClient,
		AccountProvisioner: identityService,
		PasswordReset:      signupNotifier,
		GithubLoginIDPID:   githubLoginIDPID,
		HTTPClient: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   5 * time.Second,
		},
	})
	if err != nil {
		return err
	}

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	rootMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pg.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		if err := chConn.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.RegisterZitadelActionRoutes(rootMux, identityService, zitadelActionSigningKey)
	api.RegisterBrowserAuthRoutes(rootMux, browserAuth)
	api.NewAuthPublicAPI(rootMux, api.Config{
		Version:        serviceVersion,
		Service:        identityService,
		Authz:          authzService,
		InstallationID: runtimeCfg.InstallationID,
		ProductBaseURL: runtimeCfg.BrowserAuthPublicBaseURL,
		SignupNotifier: signupNotifier,
	})

	privateMux := http.NewServeMux()
	api.NewAPI(privateMux, api.Config{
		Version:         serviceVersion,
		ListenAddr:      opts.ListenAddr,
		Service:         identityService,
		Authz:           authzService,
		InstallationID:  runtimeCfg.InstallationID,
		ProductBaseURL:  runtimeCfg.BrowserAuthPublicBaseURL,
		InviteNotifier:  inviteNotifier,
		ProviderSession: zitadelClient,
	})
	authConfig := auth.Config{
		IssuerURL: runtimeCfg.AuthIssuerURL,
		Audience:  authAudience,
	}
	protected := auth.Middleware(authConfig)(privateMux)
	rootMux.Handle("/", protected)

	internalPeerIDs, err := workloadauth.PeerIDsForSource(
		spiffeSource,
		workloadauth.ServiceAnalytics,
		workloadauth.ServiceBilling,
		workloadauth.ServiceGovernance,
		workloadauth.ServiceEmail,
		workloadauth.ServiceNotifications,
		workloadauth.ServiceObjectStorageAdmin,
		workloadauth.ServiceProfile,
		workloadauth.ServiceProjects,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
		workloadauth.ServiceSourceCodeHosting,
	)
	if err != nil {
		return err
	}
	internalTLSConfig, err := workloadauth.MTLSServerConfigForAny(spiffeSource, internalPeerIDs...)
	if err != nil {
		return fmt.Errorf("iam spiffe internal tls: %w", err)
	}
	internalMux := http.NewServeMux()
	api.NewInternalAPI(internalMux, serviceVersion, "https://"+opts.InternalListenAddr, identityService, authzService)
	profileAuthenticated := auth.Middleware(authConfig)(internalMux)
	internalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/v1/subjects/") {
			profileAuthenticated.ServeHTTP(w, r)
			return
		}
		internalMux.ServeHTTP(w, r)
	})
	internalAllowlist, err := workloadauth.ServerPeerAllowlistMiddleware(internalPeerIDs, internalHandler)
	if err != nil {
		return fmt.Errorf("iam internal allowlist: %w", err)
	}

	srv := httpserver.New(opts.ListenAddr, otelhttp.NewHandler(limitRequestBodies(rootMux, requestBodyLimit), serviceName))
	internal := httpserver.New(opts.InternalListenAddr, otelhttp.NewHandler(limitRequestBodies(internalAllowlist, requestBodyLimit), serviceName+"-internal"))
	internal.TLSConfig = internalTLSConfig
	return httpserver.RunPair(ctx, logger, srv, internal)
}

func openPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32FromInt(maxConns, "IAM_PG_MAX_CONNS")
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func int32FromInt(value int, field string) int32 {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func limitRequestBodies(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestMayHaveBody(r) {
			if r.ContentLength > maxBytes {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func requestMayHaveBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")
	default:
		return false
	}
}

type notificationInviteSender struct {
	client *notificationsinternalclient.Client
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type pwnedPasswordChecker struct {
	client *pwnedpasswords.Client
}

func (c pwnedPasswordChecker) CheckPassword(ctx context.Context, password string) (identity.BreachedPasswordCheck, error) {
	check, err := c.client.CheckPassword(ctx, password)
	if err != nil {
		return identity.BreachedPasswordCheck{}, err
	}
	return identity.BreachedPasswordCheck{Breached: check.Breached, Occurrences: check.Occurrences}, nil
}

type organizationOwnerPolicyWriter struct {
	authz *authz.Service
}

func (w organizationOwnerPolicyWriter) SetOrganizationOwner(ctx context.Context, input identity.OrganizationOwnerPolicyRequest) error {
	if w.authz == nil {
		return identity.ErrAuthzUnavailable
	}
	ownerMember := "user:" + strings.TrimSpace(input.OwnerUserID)
	current, err := w.authz.GetOrganizationPolicy(ctx, strings.TrimSpace(input.OrgID))
	if err != nil {
		return fmt.Errorf("%w: read organization policy: %v", identity.ErrAuthzUnavailable, err)
	}
	if policyHasMember(current, "roles/owner", ownerMember) {
		return nil
	}
	next := current
	next.Bindings = appendOwnerBinding(next.Bindings, ownerMember)
	if _, err := w.authz.SetOrganizationPolicy(ctx, strings.TrimSpace(input.OrgID), next, strings.TrimSpace(input.OperationID)); err != nil {
		return fmt.Errorf("%w: set organization owner policy: %v", identity.ErrAuthzUnavailable, err)
	}
	return nil
}

type billingOrganizationProvisioner struct {
	client *billingclient.Client
}

func (p billingOrganizationProvisioner) EnsureBillingOrganization(ctx context.Context, input identity.BillingOrganizationProvisioningRequest) error {
	if p.client == nil {
		return identity.ErrBillingUnavailable
	}
	var trustTier *billingclient.BillingTrustTier
	if trimmed := strings.TrimSpace(input.TrustTier); trimmed != "" {
		value := billingclient.BillingTrustTier(trimmed)
		trustTier = &value
	}
	resp, err := p.client.EnsureBillingOrganization(ctx, billingclient.EnsureBillingOrganizationRequest{
		Body: billingclient.EnsureBillingOrganizationInputBody{
			OrgID:       billingclient.OrgId(strings.TrimSpace(input.OrgID)),
			DisplayName: strings.TrimSpace(input.DisplayName),
			TrustTier:   trustTier,
		},
	})
	if err != nil {
		return fmt.Errorf("%w: ensure billing organization: %v", identity.ErrBillingUnavailable, err)
	}
	if resp == nil {
		return fmt.Errorf("%w: ensure billing organization returned no response", identity.ErrBillingUnavailable)
	}
	if resp.StatusCode != http.StatusOK || resp.Result == nil {
		return fmt.Errorf("%w: ensure billing organization status %d: %s", identity.ErrBillingUnavailable, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

func (s notificationInviteSender) SendMemberInvite(ctx context.Context, input api.MemberInviteNotification) error {
	if s.client == nil {
		return fmt.Errorf("notifications client is required")
	}
	orgName := strings.TrimSpace(input.OrgDisplayName)
	if orgName == "" {
		orgName = strings.TrimSpace(input.OrgSlug)
	}
	title := notificationsinternalclient.NotificationTitle("Join " + orgName + " on Verself")
	priority := notificationsinternalclient.NotificationPriorityNORMAL
	actionURL := notificationsinternalclient.ActionURL(strings.TrimSpace(input.ActionURL))
	email := notificationsinternalclient.EmailAddress(strings.TrimSpace(input.Email))
	var resourceName *notificationsinternalclient.ResourceName
	if trimmed := strings.TrimSpace(input.ResourceName); trimmed != "" {
		value := notificationsinternalclient.ResourceName(trimmed)
		resourceName = &value
	}
	data := map[string]any{
		"org_id":   strings.TrimSpace(input.OrgID),
		"org_slug": strings.TrimSpace(input.OrgSlug),
		"user_id":  strings.TrimSpace(input.UserID),
	}
	body := notificationsinternalclient.RequiredNotificationBody(
		fmt.Sprintf("You were invited to join %s on Verself.\n\nAccept the invite: %s\n\nIf you did not expect this invitation, ignore this email.", orgName, actionURL),
	)
	resp, err := s.client.TriggerNotificationWorkflow(ctx, notificationsinternalclient.TriggerNotificationWorkflowRequest{
		WorkflowKey:    notificationsinternalclient.WorkflowKey("iam.member.invite"),
		IdempotencyKey: notificationsinternalclient.IdempotencyKey("iam:member_invite:" + strings.TrimSpace(input.UserID)),
		Body: notificationsinternalclient.TriggerNotificationWorkflowInputBody{
			ActionURL:          &actionURL,
			Body:               body,
			Data:               &data,
			OrgID:              notificationsinternalclient.OrgId(strings.TrimSpace(input.OrgID)),
			Priority:           &priority,
			Recipients:         notificationsinternalclient.WorkflowRecipients{{Email: &email}},
			TargetResourceName: resourceName,
			Title:              &title,
		},
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("notifications workflow returned no response")
	}
	if resp.StatusCode != http.StatusAccepted || resp.Result == nil {
		return fmt.Errorf("notifications workflow status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

type notificationSignupSender struct {
	client *notificationsinternalclient.Client
}

func (s notificationSignupSender) SendSignupVerification(ctx context.Context, input api.SignupVerificationNotification) error {
	if s.client == nil {
		return fmt.Errorf("notifications client is required")
	}
	title := notificationsinternalclient.NotificationTitle("Verify your Verself signup")
	priority := notificationsinternalclient.NotificationPriorityNORMAL
	actionURL := notificationsinternalclient.ActionURL(strings.TrimSpace(input.ActionURL))
	email := notificationsinternalclient.EmailAddress(strings.TrimSpace(input.Email))
	var resourceName *notificationsinternalclient.ResourceName
	if trimmed := strings.TrimSpace(input.ResourceName); trimmed != "" {
		value := notificationsinternalclient.ResourceName(trimmed)
		resourceName = &value
	}
	data := map[string]any{
		"signup_intent_id": strings.TrimSpace(input.SignupIntentID),
		"org_id":           strings.TrimSpace(input.OrgID),
	}
	body := notificationsinternalclient.RequiredNotificationBody(
		fmt.Sprintf("Verify your email to finish creating %s on Verself.\n\nVerify signup: %s\n\nIf you did not start this signup, ignore this email.", strings.TrimSpace(input.OrganizationDisplayName), actionURL),
	)
	resp, err := s.client.TriggerNotificationWorkflow(ctx, notificationsinternalclient.TriggerNotificationWorkflowRequest{
		WorkflowKey:    notificationsinternalclient.WorkflowKey("iam.signup.verify"),
		IdempotencyKey: notificationsinternalclient.IdempotencyKey("iam:signup_verify:" + strings.TrimSpace(input.SignupIntentID) + ":" + strings.TrimSpace(input.VerificationFingerprint)),
		Body: notificationsinternalclient.TriggerNotificationWorkflowInputBody{
			ActionURL:          &actionURL,
			Body:               body,
			Data:               &data,
			OrgID:              notificationsinternalclient.OrgId(strings.TrimSpace(input.OrgID)),
			Priority:           &priority,
			Recipients:         notificationsinternalclient.WorkflowRecipients{{Email: &email}},
			TargetResourceName: resourceName,
			Title:              &title,
		},
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("notifications workflow returned no response")
	}
	if resp.StatusCode != http.StatusAccepted || resp.Result == nil {
		return fmt.Errorf("notifications workflow status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

func (s notificationSignupSender) SendSignupAccountExists(ctx context.Context, input api.SignupAccountExistsNotification) error {
	if s.client == nil {
		return fmt.Errorf("notifications client is required")
	}
	title := notificationsinternalclient.NotificationTitle("Your Verself account already exists")
	priority := notificationsinternalclient.NotificationPriorityNORMAL
	actionURL := notificationsinternalclient.ActionURL(strings.TrimSpace(input.LoginURL))
	email := notificationsinternalclient.EmailAddress(strings.TrimSpace(input.Email))
	var resourceName *notificationsinternalclient.ResourceName
	if trimmed := strings.TrimSpace(input.ResourceName); trimmed != "" {
		value := notificationsinternalclient.ResourceName(trimmed)
		resourceName = &value
	}
	data := map[string]any{
		"notice": "signup_account_exists",
	}
	body := notificationsinternalclient.RequiredNotificationBody(
		fmt.Sprintf("A Verself signup was requested for this email address, but an account already exists.\n\nSign in: %s\n\nIf you did not request this, no action is required.", actionURL),
	)
	resp, err := s.client.TriggerNotificationWorkflow(ctx, notificationsinternalclient.TriggerNotificationWorkflowRequest{
		WorkflowKey:    notificationsinternalclient.WorkflowKey("iam.signup.account_exists"),
		IdempotencyKey: notificationsinternalclient.IdempotencyKey(strings.TrimSpace(input.IdempotencyKey)),
		Body: notificationsinternalclient.TriggerNotificationWorkflowInputBody{
			ActionURL:          &actionURL,
			Body:               body,
			Data:               &data,
			OrgID:              notificationsinternalclient.OrgId(strings.TrimSpace(input.OrgID)),
			Priority:           &priority,
			Recipients:         notificationsinternalclient.WorkflowRecipients{{Email: &email}},
			TargetResourceName: resourceName,
			Title:              &title,
		},
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("notifications workflow returned no response")
	}
	if resp.StatusCode != http.StatusAccepted || resp.Result == nil {
		return fmt.Errorf("notifications workflow status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

func (s notificationSignupSender) SendPasswordReset(ctx context.Context, input api.PasswordResetNotification) error {
	if s.client == nil {
		return fmt.Errorf("notifications client is required")
	}
	title := notificationsinternalclient.NotificationTitle("Reset your Verself password")
	priority := notificationsinternalclient.NotificationPriorityNORMAL
	actionURL := notificationsinternalclient.ActionURL(strings.TrimSpace(input.ActionURL))
	email := notificationsinternalclient.EmailAddress(strings.TrimSpace(input.Email))
	var resourceName *notificationsinternalclient.ResourceName
	if trimmed := strings.TrimSpace(input.ResourceName); trimmed != "" {
		value := notificationsinternalclient.ResourceName(trimmed)
		resourceName = &value
	}
	data := map[string]any{
		"notice": "password_reset",
	}
	body := notificationsinternalclient.RequiredNotificationBody(
		fmt.Sprintf("Reset your Verself password: %s\n\nIf you did not request this, ignore this email.", actionURL),
	)
	resp, err := s.client.TriggerNotificationWorkflow(ctx, notificationsinternalclient.TriggerNotificationWorkflowRequest{
		WorkflowKey:    notificationsinternalclient.WorkflowKey("iam.password.reset"),
		IdempotencyKey: notificationsinternalclient.IdempotencyKey("iam:password_reset:" + hashText(strings.TrimSpace(input.ActionURL))),
		Body: notificationsinternalclient.TriggerNotificationWorkflowInputBody{
			OrgID:              notificationsinternalclient.OrgId(strings.TrimSpace(input.OrgID)),
			ActionURL:          &actionURL,
			Body:               body,
			Data:               &data,
			Priority:           &priority,
			Recipients:         notificationsinternalclient.WorkflowRecipients{{Email: &email}},
			TargetResourceName: resourceName,
			Title:              &title,
		},
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("notifications workflow returned no response")
	}
	if resp.StatusCode != http.StatusAccepted || resp.Result == nil {
		return fmt.Errorf("notifications workflow status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}
