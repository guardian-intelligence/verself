package jobs

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const githubActionsActorID = "system:github-actions"

var (
	ErrGitHubRunnerNotConfigured      = errors.New("sandbox-rental: github runner is not configured")
	ErrGitHubInstallationMissing      = errors.New("sandbox-rental: github app installation is not mapped")
	ErrGitHubInstallationInvalid      = errors.New("sandbox-rental: github app installation is invalid")
	ErrGitHubInstallationStateInvalid = errors.New("sandbox-rental: github app installation state is invalid")
)

type GitHubRunnerConfig struct {
	AppID         int64
	AppSlug       string
	ClientID      string
	ClientSecret  string
	PrivateKeyPEM string
	WebhookSecret string
	APIBaseURL    string
	WebBaseURL    string
	RunnerGroupID int64
}

type GitHubRunner struct {
	service       *Service
	appID         int64
	appSlug       string
	clientID      string
	clientSecret  string
	privateKey    *rsa.PrivateKey
	webhookSecret string
	apiBaseURL    string
	webBaseURL    string
	runnerGroupID int64
	httpClient    *http.Client

	mu     sync.Mutex
	tokens map[int64]githubInstallationToken
}

type githubInstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

type GitHubWorkflowJobWebhook struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Organization struct {
		Login string `json:"login"`
	} `json:"organization"`
	Repository struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		CloneURL      string `json:"clone_url"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	WorkflowJob struct {
		ID         int64    `json:"id"`
		RunID      int64    `json:"run_id"`
		RunAttempt int      `json:"run_attempt"`
		Name       string   `json:"name"`
		HeadBranch string   `json:"head_branch"`
		HeadSHA    string   `json:"head_sha"`
		Status     string   `json:"status"`
		Conclusion string   `json:"conclusion"`
		HTMLURL    string   `json:"html_url"`
		Labels     []string `json:"labels"`
	} `json:"workflow_job"`
}

type GitHubWorkflowJobResult struct {
	Status      string
	Reason      string
	ExecutionID uuid.UUID
	RunnerClass string
}

type githubAppInstallation struct {
	InstallationID int64
	OrgID          uint64
	AccountLogin   string
	Active         bool
}

type GitHubInstallationRecord struct {
	InstallationID int64
	OrgID          uint64
	AccountLogin   string
	AccountType    string
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GitHubInstallationConnect struct {
	State     string
	SetupURL  string
	ExpiresAt time.Time
}

type githubInstallationState struct {
	State     string
	OrgID     uint64
	ActorID   string
	ExpiresAt time.Time
}

func NewGitHubRunner(service *Service, cfg GitHubRunnerConfig, httpClient *http.Client) (*GitHubRunner, error) {
	cfg.APIBaseURL = strings.TrimSpace(cfg.APIBaseURL)
	cfg.WebBaseURL = strings.TrimSpace(cfg.WebBaseURL)
	cfg.AppSlug = strings.Trim(strings.TrimSpace(cfg.AppSlug), "/")
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	cfg.PrivateKeyPEM = strings.TrimSpace(cfg.PrivateKeyPEM)
	cfg.WebhookSecret = strings.TrimSpace(cfg.WebhookSecret)
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.github.com"
	}
	if cfg.WebBaseURL == "" {
		cfg.WebBaseURL = "https://github.com"
	}
	if cfg.RunnerGroupID == 0 {
		cfg.RunnerGroupID = 1
	}
	if cfg.AppID == 0 && cfg.AppSlug == "" && cfg.ClientID == "" && cfg.ClientSecret == "" && cfg.PrivateKeyPEM == "" && cfg.WebhookSecret == "" {
		return nil, nil
	}
	if service == nil {
		return nil, fmt.Errorf("github runner requires jobs service")
	}
	if cfg.AppID <= 0 {
		return nil, fmt.Errorf("github app id is required")
	}
	if cfg.AppSlug == "" {
		return nil, fmt.Errorf("github app slug is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("github app client id is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("github app client secret is required")
	}
	if cfg.PrivateKeyPEM == "" {
		return nil, fmt.Errorf("github app private key is required")
	}
	if cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("github app webhook secret is required")
	}
	privateKey, err := parseRSAPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	if _, err := url.ParseRequestURI(cfg.APIBaseURL); err != nil {
		return nil, fmt.Errorf("github api base url: %w", err)
	}
	if _, err := url.ParseRequestURI(cfg.WebBaseURL); err != nil {
		return nil, fmt.Errorf("github web base url: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &GitHubRunner{
		service:       service,
		appID:         cfg.AppID,
		appSlug:       cfg.AppSlug,
		clientID:      cfg.ClientID,
		clientSecret:  cfg.ClientSecret,
		privateKey:    privateKey,
		webhookSecret: cfg.WebhookSecret,
		apiBaseURL:    strings.TrimRight(cfg.APIBaseURL, "/"),
		webBaseURL:    strings.TrimRight(cfg.WebBaseURL, "/"),
		runnerGroupID: cfg.RunnerGroupID,
		httpClient:    httpClient,
		tokens:        make(map[int64]githubInstallationToken),
	}, nil
}

func (r *GitHubRunner) Configured() bool {
	return r != nil &&
		r.privateKey != nil &&
		strings.TrimSpace(r.webhookSecret) != "" &&
		strings.TrimSpace(r.appSlug) != "" &&
		strings.TrimSpace(r.clientID) != "" &&
		strings.TrimSpace(r.clientSecret) != ""
}

func (r *GitHubRunner) VerifyWebhookSignature(body []byte, provided string) bool {
	if !r.Configured() {
		return false
	}
	provided = strings.TrimSpace(strings.ToLower(provided))
	provided = strings.TrimPrefix(provided, "sha256=")
	if provided == "" || len(provided) != sha256.Size*2 {
		return false
	}
	mac := hmacSHA256Hex([]byte(r.webhookSecret), body)
	return subtleConstantTimeStringEqual(mac, provided)
}

func (r *GitHubRunner) BeginInstallation(ctx context.Context, orgID uint64, actorID string) (connect GitHubInstallationConnect, err error) {
	if !r.Configured() {
		return GitHubInstallationConnect{}, ErrGitHubRunnerNotConfigured
	}
	actorID = strings.TrimSpace(actorID)
	if orgID == 0 || actorID == "" {
		return GitHubInstallationConnect{}, ErrGitHubInstallationStateInvalid
	}
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.installation_begin")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(
			attribute.Int64("org.id", int64(orgID)),
			attribute.String("actor.id", actorID),
		)
		span.End()
	}()

	state, err := randomGitHubInstallState()
	if err != nil {
		return GitHubInstallationConnect{}, err
	}
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	if err := r.service.insertGitHubInstallationState(ctx, state, orgID, actorID, expiresAt); err != nil {
		return GitHubInstallationConnect{}, err
	}
	setupURL := r.webBaseURL + "/apps/" + url.PathEscape(r.appSlug) + "/installations/new?state=" + url.QueryEscape(state)
	return GitHubInstallationConnect{State: state, SetupURL: setupURL, ExpiresAt: expiresAt}, nil
}

func (r *GitHubRunner) CompleteInstallation(ctx context.Context, state string, code string, installationID int64) (record GitHubInstallationRecord, err error) {
	if !r.Configured() {
		return GitHubInstallationRecord{}, ErrGitHubRunnerNotConfigured
	}
	state = strings.TrimSpace(state)
	code = strings.TrimSpace(code)
	if state == "" || code == "" || installationID <= 0 {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.installation_complete")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(
			attribute.Int64("github.installation_id", installationID),
			attribute.Int64("org.id", int64(record.OrgID)),
			attribute.String("github.account_login", record.AccountLogin),
			attribute.String("github.account_type", record.AccountType),
		)
		span.End()
	}()

	pending, err := r.service.claimGitHubInstallationState(ctx, state, installationID)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	token, err := r.exchangeUserAccessToken(ctx, code)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	// The setup callback query is browser-controlled; this user-token probe ties
	// the installation ID back to the GitHub user who completed the setup flow.
	if err := r.verifyUserInstallationAccess(ctx, token, installationID); err != nil {
		return GitHubInstallationRecord{}, err
	}
	installation, err := r.fetchInstallation(ctx, installationID)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	record, err = r.service.upsertGitHubInstallation(ctx, pending.OrgID, installation)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := r.service.completeGitHubInstallationState(ctx, state, installationID, time.Now().UTC()); err != nil {
		return GitHubInstallationRecord{}, err
	}
	return record, nil
}

func (r *GitHubRunner) HandleWorkflowJob(ctx context.Context, deliveryID string, payload GitHubWorkflowJobWebhook) (result GitHubWorkflowJobResult, err error) {
	if !r.Configured() {
		return GitHubWorkflowJobResult{}, ErrGitHubRunnerNotConfigured
	}
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.workflow_job")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(
			attribute.String("github.delivery_id", deliveryID),
			attribute.String("github.action", payload.Action),
			attribute.Int64("github.installation_id", payload.Installation.ID),
			attribute.Int64("github.workflow_job_id", payload.WorkflowJob.ID),
			attribute.String("github.repository", payload.Repository.FullName),
			attribute.String("github.runner_class", result.RunnerClass),
		)
		span.End()
	}()

	switch strings.TrimSpace(payload.Action) {
	case "queued":
		return r.handleQueuedWorkflowJob(ctx, deliveryID, payload)
	case "completed":
		if err := r.service.markGitHubWorkflowJobCompleted(ctx, payload.WorkflowJob.ID, deliveryID, time.Now().UTC()); err != nil {
			return GitHubWorkflowJobResult{}, err
		}
		return GitHubWorkflowJobResult{Status: "completed"}, nil
	default:
		return GitHubWorkflowJobResult{Status: "ignored", Reason: "unsupported action"}, nil
	}
}

func (r *GitHubRunner) handleQueuedWorkflowJob(ctx context.Context, deliveryID string, payload GitHubWorkflowJobWebhook) (GitHubWorkflowJobResult, error) {
	installation, err := r.service.lookupGitHubInstallation(ctx, payload.Installation.ID)
	if err != nil {
		return GitHubWorkflowJobResult{}, err
	}
	runnerClass, ok := selectRunnerClass(payload.WorkflowJob.Labels)
	if !ok {
		return GitHubWorkflowJobResult{Status: "ignored", Reason: "no forge metal runner label"}, nil
	}
	claimed, err := r.service.claimGitHubWorkflowJob(ctx, installation.OrgID, deliveryID, payload, runnerClass)
	if err != nil {
		return GitHubWorkflowJobResult{}, err
	}
	if !claimed {
		existing, ok, loadErr := r.service.lookupGitHubWorkflowJobExecution(ctx, payload.WorkflowJob.ID)
		if loadErr != nil {
			return GitHubWorkflowJobResult{}, loadErr
		}
		result := GitHubWorkflowJobResult{Status: "duplicate", RunnerClass: runnerClass}
		if ok {
			result.ExecutionID = existing
		}
		return result, nil
	}

	jit, err := r.createJITConfig(ctx, payload.Installation.ID, payload.Organization.Login, payload.WorkflowJob.ID, runnerClass)
	if err != nil {
		_ = r.service.markGitHubWorkflowJobFailed(ctx, payload.WorkflowJob.ID, err)
		return GitHubWorkflowJobResult{}, err
	}

	executionID, _, err := r.service.Submit(ctx, installation.OrgID, githubActionsActorID, SubmitRequest{
		Kind:             WorkloadKindGitHubRunner,
		SourceKind:       SourceKindGitHubAction,
		WorkloadKind:     WorkloadKindGitHubRunner,
		SourceRef:        githubWorkflowJobSourceRef(payload),
		RunnerClass:      runnerClass,
		ExternalProvider: "github",
		ExternalTaskID:   strconv.FormatInt(payload.WorkflowJob.ID, 10),
		ProductID:        defaultProductID,
		Provider:         "github",
		IdempotencyKey:   fmt.Sprintf("github:%d:%d", payload.Installation.ID, payload.WorkflowJob.ID),
		Repo:             strings.TrimSpace(payload.Repository.FullName),
		RepoURL:          strings.TrimSpace(payload.Repository.CloneURL),
		Ref:              firstNonEmpty(payload.WorkflowJob.HeadSHA, payload.WorkflowJob.HeadBranch),
		DefaultBranch:    strings.TrimSpace(payload.Repository.DefaultBranch),
		GitHubJITConfig:  jit,
	})
	if err != nil {
		_ = r.service.markGitHubWorkflowJobFailed(ctx, payload.WorkflowJob.ID, err)
		return GitHubWorkflowJobResult{}, err
	}
	if err := r.service.attachGitHubWorkflowExecution(ctx, payload.WorkflowJob.ID, executionID, time.Now().UTC()); err != nil {
		return GitHubWorkflowJobResult{}, err
	}
	return GitHubWorkflowJobResult{Status: "submitted", ExecutionID: executionID, RunnerClass: runnerClass}, nil
}

type githubInstallationAPIRecord struct {
	InstallationID int64
	AccountLogin   string
	AccountType    string
}

func (r *GitHubRunner) fetchInstallation(ctx context.Context, installationID int64) (record githubInstallationAPIRecord, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.get_installation")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Int64("github.installation_id", installationID))
		span.End()
	}()

	jwt, err := r.appJWT(time.Now().UTC())
	if err != nil {
		return githubInstallationAPIRecord{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/app/installations/%d", r.apiBaseURL, installationID), nil)
	if err != nil {
		return githubInstallationAPIRecord{}, err
	}
	setGitHubJSONHeaders(req, jwt)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return githubInstallationAPIRecord{}, fmt.Errorf("github get installation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return githubInstallationAPIRecord{}, fmt.Errorf("github get installation: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		SuspendedAt *time.Time `json:"suspended_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return githubInstallationAPIRecord{}, fmt.Errorf("decode github installation response: %w", err)
	}
	accountType := strings.TrimSpace(out.Account.Type)
	if out.ID != installationID || strings.TrimSpace(out.Account.Login) == "" || out.SuspendedAt != nil || accountType != "Organization" {
		return githubInstallationAPIRecord{}, ErrGitHubInstallationInvalid
	}
	return githubInstallationAPIRecord{
		InstallationID: out.ID,
		AccountLogin:   strings.TrimSpace(out.Account.Login),
		AccountType:    accountType,
	}, nil
}

func (r *GitHubRunner) createJITConfig(ctx context.Context, installationID int64, org string, jobID int64, runnerClass string) (jitConfig string, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.create_jit_config")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(
			attribute.Int64("github.installation_id", installationID),
			attribute.String("github.org", org),
			attribute.String("github.runner_class", runnerClass),
		)
		span.End()
	}()

	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		return "", err
	}
	org = strings.TrimSpace(org)
	if org == "" {
		return "", fmt.Errorf("github organization login is required")
	}
	body, err := json.Marshal(map[string]any{
		"name":            fmt.Sprintf("forge-metal-%d", jobID),
		"runner_group_id": r.runnerGroupID,
		"labels":          []string{"self-hosted", "linux", "x64", runnerClass},
		"work_folder":     "_work",
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.apiBaseURL+"/orgs/"+url.PathEscape(org)+"/actions/runners/generate-jitconfig", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setGitHubJSONHeaders(req, token)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github generate jitconfig: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github generate jitconfig: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		EncodedJITConfig string `json:"encoded_jit_config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode github jitconfig response: %w", err)
	}
	if strings.TrimSpace(out.EncodedJITConfig) == "" {
		return "", fmt.Errorf("github jitconfig response missing encoded_jit_config")
	}
	return out.EncodedJITConfig, nil
}

func (r *GitHubRunner) exchangeUserAccessToken(ctx context.Context, code string) (token string, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.exchange_user_token")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	body := url.Values{}
	body.Set("client_id", r.clientID)
	body.Set("client_secret", r.clientSecret)
	body.Set("code", code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.webBaseURL+"/login/oauth/access_token", strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github exchange user token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github exchange user token: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode github user token response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("github exchange user token: %s %s", out.Error, out.Description)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", fmt.Errorf("github user token response missing access_token")
	}
	return strings.TrimSpace(out.AccessToken), nil
}

func (r *GitHubRunner) verifyUserInstallationAccess(ctx context.Context, token string, installationID int64) (err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.github_runner.verify_user_installation")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attribute.Int64("github.installation_id", installationID))
		span.End()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/user/installations/%d/repositories?per_page=1", r.apiBaseURL, installationID), nil)
	if err != nil {
		return err
	}
	setGitHubJSONHeaders(req, token)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github verify user installation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github verify user installation: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func (r *GitHubRunner) installationToken(ctx context.Context, installationID int64) (string, error) {
	now := time.Now().UTC()
	r.mu.Lock()
	if cached, ok := r.tokens[installationID]; ok && cached.ExpiresAt.After(now.Add(5*time.Minute)) {
		token := cached.Token
		r.mu.Unlock()
		return token, nil
	}
	r.mu.Unlock()

	jwt, err := r.appJWT(now)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", r.apiBaseURL, installationID), nil)
	if err != nil {
		return "", err
	}
	setGitHubJSONHeaders(req, jwt)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github installation token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("github installation token: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode github installation token: %w", err)
	}
	if strings.TrimSpace(out.Token) == "" {
		return "", fmt.Errorf("github installation token response missing token")
	}
	r.mu.Lock()
	r.tokens[installationID] = githubInstallationToken{Token: out.Token, ExpiresAt: out.ExpiresAt}
	r.mu.Unlock()
	return out.Token, nil
}

func (r *GitHubRunner) appJWT(now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-1 * time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": r.appID,
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := header + "." + payload
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, r.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Service) lookupGitHubInstallation(ctx context.Context, installationID int64) (githubAppInstallation, error) {
	var out githubAppInstallation
	var orgID int64
	if err := s.PG.QueryRowContext(ctx, `
		SELECT installation_id, org_id, account_login, active
		FROM github_app_installations
		WHERE installation_id = $1
	`, installationID).Scan(&out.InstallationID, &orgID, &out.AccountLogin, &out.Active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return githubAppInstallation{}, ErrGitHubInstallationMissing
		}
		return githubAppInstallation{}, fmt.Errorf("lookup github installation: %w", err)
	}
	if !out.Active {
		return githubAppInstallation{}, ErrGitHubInstallationMissing
	}
	out.OrgID = uint64(orgID)
	return out, nil
}

func (s *Service) upsertGitHubInstallation(ctx context.Context, orgID uint64, installation githubInstallationAPIRecord) (GitHubInstallationRecord, error) {
	var record GitHubInstallationRecord
	var scannedOrgID int64
	if err := s.PG.QueryRowContext(ctx, `
		INSERT INTO github_app_installations (
			installation_id, org_id, account_login, account_type, active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, true, now(), now())
		ON CONFLICT (installation_id) DO UPDATE
		SET org_id = EXCLUDED.org_id,
		    account_login = EXCLUDED.account_login,
		    account_type = EXCLUDED.account_type,
		    active = true,
		    updated_at = now()
		RETURNING installation_id, org_id, account_login, account_type, active, created_at, updated_at
	`, installation.InstallationID, int64(orgID), installation.AccountLogin, installation.AccountType).Scan(
		&record.InstallationID,
		&scannedOrgID,
		&record.AccountLogin,
		&record.AccountType,
		&record.Active,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return GitHubInstallationRecord{}, fmt.Errorf("upsert github installation: %w", err)
	}
	record.OrgID = uint64(scannedOrgID)
	return record, nil
}

func (s *Service) insertGitHubInstallationState(ctx context.Context, state string, orgID uint64, actorID string, expiresAt time.Time) error {
	if _, err := s.PG.ExecContext(ctx, `
		INSERT INTO github_app_installation_states (
			state, org_id, actor_id, expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, now(), now())
	`, state, int64(orgID), actorID, expiresAt); err != nil {
		return fmt.Errorf("insert github installation state: %w", err)
	}
	return nil
}

func (s *Service) claimGitHubInstallationState(ctx context.Context, state string, installationID int64) (githubInstallationState, error) {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return githubInstallationState{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var pending githubInstallationState
	var rawOrgID int64
	var completedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT state, org_id, actor_id, expires_at, completed_at
		FROM github_app_installation_states
		WHERE state = $1
		FOR UPDATE
	`, state).Scan(&pending.State, &rawOrgID, &pending.ActorID, &pending.ExpiresAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return githubInstallationState{}, ErrGitHubInstallationStateInvalid
		}
		return githubInstallationState{}, fmt.Errorf("claim github installation state: %w", err)
	}
	if rawOrgID <= 0 || completedAt.Valid || !pending.ExpiresAt.After(time.Now().UTC()) {
		return githubInstallationState{}, ErrGitHubInstallationStateInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE github_app_installation_states
		SET installation_id = $2, updated_at = now()
		WHERE state = $1
	`, state, installationID); err != nil {
		return githubInstallationState{}, fmt.Errorf("update github installation state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return githubInstallationState{}, err
	}
	pending.OrgID = uint64(rawOrgID)
	return pending, nil
}

func (s *Service) completeGitHubInstallationState(ctx context.Context, state string, installationID int64, completedAt time.Time) error {
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE github_app_installation_states
		SET installation_id = $2, completed_at = $3, updated_at = $3
		WHERE state = $1
		  AND completed_at IS NULL
	`, state, installationID, completedAt); err != nil {
		return fmt.Errorf("complete github installation state: %w", err)
	}
	return nil
}

func (s *Service) ListGitHubInstallations(ctx context.Context, orgID uint64) ([]GitHubInstallationRecord, error) {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT installation_id, org_id, account_login, account_type, active, created_at, updated_at
		FROM github_app_installations
		WHERE org_id = $1
		ORDER BY updated_at DESC, installation_id DESC
	`, int64(orgID))
	if err != nil {
		return nil, fmt.Errorf("list github installations: %w", err)
	}
	defer rows.Close()

	records := make([]GitHubInstallationRecord, 0)
	for rows.Next() {
		var record GitHubInstallationRecord
		var scannedOrgID int64
		if err := rows.Scan(
			&record.InstallationID,
			&scannedOrgID,
			&record.AccountLogin,
			&record.AccountType,
			&record.Active,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan github installation: %w", err)
		}
		record.OrgID = uint64(scannedOrgID)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan github installations: %w", err)
	}
	return records, nil
}

func (s *Service) claimGitHubWorkflowJob(ctx context.Context, orgID uint64, deliveryID string, payload GitHubWorkflowJobWebhook, runnerClass string) (claimed bool, err error) {
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO github_workflow_job_executions (
			github_job_id, installation_id, org_id, org_login, repo_id, repo_full_name,
			runner_class, delivery_id, action, state, queued_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'queued', $10, $10, $10)
		ON CONFLICT (github_job_id) DO NOTHING
	`, payload.WorkflowJob.ID, payload.Installation.ID, int64(orgID), payload.Organization.Login, payload.Repository.ID,
		payload.Repository.FullName, runnerClass, deliveryID, payload.Action, now); err != nil {
		return false, err
	}
	var existingExecution string
	var state string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(execution_id::text, ''), state
		FROM github_workflow_job_executions
		WHERE github_job_id = $1
		FOR UPDATE
	`, payload.WorkflowJob.ID).Scan(&existingExecution, &state); err != nil {
		return false, err
	}
	if existingExecution != "" || state == "provisioning" || state == "submitted" || state == "completed" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE github_workflow_job_executions
			SET delivery_id = $2, action = $3, updated_at = $4
			WHERE github_job_id = $1
		`, payload.WorkflowJob.ID, deliveryID, payload.Action, now); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE github_workflow_job_executions
		SET state = 'provisioning', delivery_id = $2, action = $3, updated_at = $4
		WHERE github_job_id = $1
	`, payload.WorkflowJob.ID, deliveryID, payload.Action, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) lookupGitHubWorkflowJobExecution(ctx context.Context, githubJobID int64) (uuid.UUID, bool, error) {
	var raw string
	if err := s.PG.QueryRowContext(ctx, `
		SELECT COALESCE(execution_id::text, '')
		FROM github_workflow_job_executions
		WHERE github_job_id = $1
	`, githubJobID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, true, nil
	}
	executionID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false, err
	}
	return executionID, true, nil
}

func (s *Service) attachGitHubWorkflowExecution(ctx context.Context, githubJobID int64, executionID uuid.UUID, now time.Time) error {
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE github_workflow_job_executions
		SET execution_id = $2, state = 'submitted', submitted_at = $3, updated_at = $3, last_error = ''
		WHERE github_job_id = $1
	`, githubJobID, executionID, now); err != nil {
		return fmt.Errorf("attach github workflow execution: %w", err)
	}
	return nil
}

func (s *Service) markGitHubWorkflowJobFailed(ctx context.Context, githubJobID int64, cause error) error {
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE github_workflow_job_executions
		SET state = 'failed', last_error = $2, updated_at = $3
		WHERE github_job_id = $1
		  AND execution_id IS NULL
	`, githubJobID, safeErrorText(cause), time.Now().UTC()); err != nil {
		return fmt.Errorf("mark github workflow job failed: %w", err)
	}
	return nil
}

func (s *Service) markGitHubWorkflowJobCompleted(ctx context.Context, githubJobID int64, deliveryID string, now time.Time) error {
	if _, err := s.PG.ExecContext(ctx, `
		UPDATE github_workflow_job_executions
		SET state = 'completed', delivery_id = $2, finalized_at = $3, updated_at = $3
		WHERE github_job_id = $1
	`, githubJobID, deliveryID, now); err != nil {
		return fmt.Errorf("mark github workflow job completed: %w", err)
	}
	return nil
}

func selectRunnerClass(labels []string) (string, bool) {
	for _, label := range labels {
		if strings.TrimSpace(label) == DefaultRunnerClassLabel {
			return DefaultRunnerClassLabel, true
		}
	}
	return "", false
}

func githubWorkflowJobSourceRef(payload GitHubWorkflowJobWebhook) string {
	if strings.TrimSpace(payload.WorkflowJob.HTMLURL) != "" {
		return payload.WorkflowJob.HTMLURL
	}
	return fmt.Sprintf("github://%s/actions/jobs/%d", strings.TrimSpace(payload.Repository.FullName), payload.WorkflowJob.ID)
}

func safeErrorText(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 4000 {
		return text[:4000]
	}
	return text
}

func randomGitHubInstallState() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("github installation state entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func parseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("github app private key must be PEM encoded")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github app private key must be RSA")
	}
	return key, nil
}

func setGitHubJSONHeaders(req *http.Request, bearer string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
}

func hmacSHA256Hex(key, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	return fmt.Sprintf("%x", mac.Sum(nil))
}

func subtleConstantTimeStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
