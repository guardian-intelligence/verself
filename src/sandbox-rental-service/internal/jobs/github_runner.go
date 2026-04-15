package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	ErrGitHubRunnerNotConfigured      = errors.New("github runner is not configured")
	ErrGitHubInstallationInvalid      = errors.New("github installation is invalid")
	ErrGitHubInstallationStateInvalid = errors.New("github installation state is invalid")
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
	service *Service
	cfg     GitHubRunnerConfig
	client  *http.Client
}

type GitHubInstallationConnect struct {
	State     string
	SetupURL  string
	ExpiresAt time.Time
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

type GitHubWorkflowJobWebhook struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
	WorkflowJob struct {
		ID          int64     `json:"id"`
		RunID       int64     `json:"run_id"`
		Name        string    `json:"name"`
		Status      string    `json:"status"`
		Conclusion  string    `json:"conclusion"`
		Labels      []string  `json:"labels"`
		RunnerID    int64     `json:"runner_id"`
		RunnerName  string    `json:"runner_name"`
		StartedAt   time.Time `json:"started_at"`
		CompletedAt time.Time `json:"completed_at"`
	} `json:"workflow_job"`
}

func NewGitHubRunner(service *Service, cfg GitHubRunnerConfig, client *http.Client) (*GitHubRunner, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return &GitHubRunner{service: service, cfg: cfg, client: client}, nil
}

func (r *GitHubRunner) Configured() bool {
	return r != nil && r.cfg.AppID != 0 && strings.TrimSpace(r.cfg.AppSlug) != "" && strings.TrimSpace(r.cfg.ClientID) != ""
}

func (r *GitHubRunner) BeginInstallation(ctx context.Context, orgID uint64, actorID string) (GitHubInstallationConnect, error) {
	if !r.Configured() {
		return GitHubInstallationConnect{}, ErrGitHubRunnerNotConfigured
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return GitHubInstallationConnect{}, err
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	if r.service != nil && r.service.PGX != nil {
		_, _ = r.service.PGX.Exec(ctx, `INSERT INTO github_installation_states (state, org_id, actor_id, expires_at, created_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (state) DO NOTHING`, state, orgID, actorID, expiresAt, time.Now().UTC())
	}
	webBase := strings.TrimRight(firstNonEmpty(r.cfg.WebBaseURL, "https://github.com"), "/")
	return GitHubInstallationConnect{
		State:     state,
		SetupURL:  fmt.Sprintf("%s/apps/%s/installations/new?state=%s", webBase, r.cfg.AppSlug, state),
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) ListGitHubInstallations(ctx context.Context, orgID uint64) ([]GitHubInstallationRecord, error) {
	if s.PGX == nil {
		return nil, nil
	}
	rows, err := s.PGX.Query(ctx, `SELECT installation_id, org_id, account_login, account_type, active, created_at, updated_at FROM github_installations WHERE org_id = $1 ORDER BY updated_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GitHubInstallationRecord{}
	for rows.Next() {
		var row GitHubInstallationRecord
		if err := rows.Scan(&row.InstallationID, &row.OrgID, &row.AccountLogin, &row.AccountType, &row.Active, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *GitHubRunner) CompleteInstallation(ctx context.Context, state, code string, installationID int64) (GitHubInstallationRecord, error) {
	_ = strings.TrimSpace(code)
	if !r.Configured() {
		return GitHubInstallationRecord{}, ErrGitHubRunnerNotConfigured
	}
	state = strings.TrimSpace(state)
	if state == "" || installationID <= 0 {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	if r.service == nil || r.service.PGX == nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationInvalid
	}
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return GitHubInstallationRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		orgID   uint64
		actorID string
		expires time.Time
	)
	if err := tx.QueryRow(ctx, `SELECT org_id, actor_id, expires_at FROM github_installation_states WHERE state = $1 FOR UPDATE`, state).Scan(&orgID, &actorID, &expires); err != nil {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	if time.Now().UTC().After(expires) {
		return GitHubInstallationRecord{}, ErrGitHubInstallationStateInvalid
	}
	now := time.Now().UTC()
	var record GitHubInstallationRecord
	if err := tx.QueryRow(ctx, `INSERT INTO github_installations (
		installation_id, org_id, account_login, account_type, active, created_at, updated_at
	) VALUES ($1,$2,$3,$4,true,$5,$5)
	ON CONFLICT (installation_id) DO UPDATE SET
		org_id = EXCLUDED.org_id,
		active = true,
		updated_at = EXCLUDED.updated_at
	RETURNING installation_id, org_id, account_login, account_type, active, created_at, updated_at`,
		installationID, orgID, actorID, "organization", now).Scan(&record.InstallationID, &record.OrgID, &record.AccountLogin, &record.AccountType, &record.Active, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM github_installation_states WHERE state = $1`, state); err != nil {
		return GitHubInstallationRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GitHubInstallationRecord{}, err
	}
	return record, nil
}

func (r *GitHubRunner) VerifyWebhookSignature(payload []byte, signature string) bool {
	return r != nil && verifyGitHubSignature(r.cfg.WebhookSecret, payload, signature) == nil
}

func (r *GitHubRunner) HandleWebhook(ctx context.Context, eventName string, deliveryID string, payload []byte, signature string) error {
	if !r.Configured() {
		return ErrGitHubRunnerNotConfigured
	}
	if strings.TrimSpace(r.cfg.WebhookSecret) == "" {
		return fmt.Errorf("github webhook secret is not configured")
	}
	if err := verifyGitHubSignature(r.cfg.WebhookSecret, payload, signature); err != nil {
		return err
	}
	if eventName != "workflow_job" {
		return nil
	}
	var event GitHubWorkflowJobWebhook
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	if r.service == nil || r.service.PGX == nil {
		return nil
	}
	labels, _ := json.Marshal(event.WorkflowJob.Labels)
	_, err := r.service.PGX.Exec(ctx, `INSERT INTO github_workflow_jobs (
		github_job_id, installation_id, repository_id, repository_full_name, run_id,
		status, conclusion, labels_json, runner_id, runner_name, last_webhook_delivery, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	ON CONFLICT (github_job_id) DO UPDATE SET
		status = EXCLUDED.status,
		conclusion = EXCLUDED.conclusion,
		labels_json = EXCLUDED.labels_json,
		runner_id = EXCLUDED.runner_id,
		runner_name = EXCLUDED.runner_name,
		last_webhook_delivery = EXCLUDED.last_webhook_delivery,
		updated_at = EXCLUDED.updated_at`,
		event.WorkflowJob.ID, event.Installation.ID, event.Repository.ID, event.Repository.FullName, event.WorkflowJob.RunID,
		event.WorkflowJob.Status, event.WorkflowJob.Conclusion, string(labels), event.WorkflowJob.RunnerID, event.WorkflowJob.RunnerName, deliveryID, time.Now().UTC())
	return err
}

func verifyGitHubSignature(secret string, payload []byte, signature string) error {
	secret = strings.TrimSpace(secret)
	signature = strings.TrimSpace(signature)
	if secret == "" {
		return nil
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return fmt.Errorf("missing github webhook signature")
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return fmt.Errorf("decode github webhook signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return fmt.Errorf("invalid github webhook signature")
	}
	return nil
}
