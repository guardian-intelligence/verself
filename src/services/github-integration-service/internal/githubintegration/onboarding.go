package githubintegration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/github-integration-service/internal/store"
	secretsclient "github.com/verself/secrets-service/client"
)

const (
	setupSessionTTL = 30 * time.Minute
	oauthSessionTTL = 15 * time.Minute
)

var (
	ErrOAuthNotConfigured          = errors.New("github oauth is not configured")
	ErrSetupNotConfigured          = errors.New("github app setup is not configured")
	ErrInvalidSetupState           = errors.New("github setup state is invalid")
	ErrGitHubUserAuthorization     = errors.New("github user authorization is required")
	ErrInstallationAlreadyBound    = errors.New("github installation is already bound")
	ErrGitHubInstallationForbidden = errors.New("github installation is not accessible to the authorized user")
	ErrInvalidPageToken            = errors.New("github page token is invalid")
)

type Principal struct {
	OrgID   string
	ActorID string
	Email   string
}

type SetupSession struct {
	ID                     uuid.UUID
	OrgID                  string
	ActorID                string
	State                  string
	InstallationURL        string
	CallbackURL            string
	ProviderInstallationID int64
	UserAuthorizationID    uuid.UUID
	InstallationBindingID  uuid.UUID
	ExpiresAt              time.Time
	CompletedAt            time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type UserAuthorization struct {
	ID             uuid.UUID
	OrgID          string
	ActorID        string
	ProviderUserID int64
	GitHubLogin    string
	Scopes         []string
	CredentialRef  string
	State          string
	AuthorizedAt   time.Time
	LastVerifiedAt time.Time
	RevokedAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type InstallationBinding struct {
	ID                     uuid.UUID
	OrgID                  string
	ProviderInstallationID int64
	ProviderAccountID      int64
	AccountLogin           string
	AccountType            string
	AppSlug                string
	RepositorySelection    string
	ConfigurationURL       string
	SetupSessionID         uuid.UUID
	ConnectedByActorID     string
	State                  string
	ConnectedAt            time.Time
	DisconnectedAt         time.Time
	LastSyncedAt           time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type RepositoryBinding struct {
	ID                     uuid.UUID
	OrgID                  string
	InstallationBindingID  uuid.UUID
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	OwnerLogin             string
	RepositoryName         string
	RepositoryFullName     string
	DefaultBranch          string
	Private                bool
	State                  string
	EnabledByActorID       string
	DisabledByActorID      string
	EnabledAt              time.Time
	DisabledAt             time.Time
	ObservedFromAPIAt      time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type RepositoryCandidate struct {
	ProviderInstallationID      int64
	ProviderRepositoryID        int64
	OwnerLogin                  string
	RepositoryName              string
	RepositoryFullName          string
	DefaultBranch               string
	Private                     bool
	InstallationRepositoryState string
	RepositoryBindingID         uuid.UUID
	RepositoryBindingState      string
	ObservedFromAPIAt           time.Time
}

type InstallationListPage struct {
	Installations []InstallationBinding
	NextPageToken string
}

type RepositoryListPage struct {
	Repositories  []RepositoryCandidate
	NextPageToken string
}

type StartSetupResult struct {
	Session SetupSession
	State   string
}

type StartOAuthResult struct {
	OAuthSessionID   uuid.UUID
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

type githubInstallationResponse struct {
	ID                  int64           `json:"id"`
	AppSlug             string          `json:"app_slug"`
	TargetType          string          `json:"target_type"`
	RepositorySelection string          `json:"repository_selection"`
	AccessTokensURL     string          `json:"access_tokens_url"`
	RepositoriesURL     string          `json:"repositories_url"`
	HTMLURL             string          `json:"html_url"`
	Permissions         json.RawMessage `json:"permissions"`
	SuspendedAt         *time.Time      `json:"suspended_at"`
	Account             githubAccount   `json:"account"`
}

type githubAccount struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Type      string `json:"type"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type githubRepositoryResponse struct {
	ID            int64         `json:"id"`
	Name          string        `json:"name"`
	FullName      string        `json:"full_name"`
	DefaultBranch string        `json:"default_branch"`
	Private       bool          `json:"private"`
	Archived      bool          `json:"archived"`
	Visibility    string        `json:"visibility"`
	HTMLURL       string        `json:"html_url"`
	Owner         githubAccount `json:"owner"`
}

type githubInstallationRepositoriesResponse struct {
	TotalCount   int                        `json:"total_count"`
	Repositories []githubRepositoryResponse `json:"repositories"`
}

type githubUserInstallationsResponse struct {
	TotalCount    int                          `json:"total_count"`
	Installations []githubInstallationResponse `json:"installations"`
}

type githubOAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

type githubUserResponse struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
	Type      string `json:"type"`
}

func (s *Service) StartSetupSession(ctx context.Context, principal Principal, idempotencyKey string, callbackURL string) (StartSetupResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return StartSetupResult{}, err
	}
	if strings.TrimSpace(s.setupURL()) == "" {
		return StartSetupResult{}, ErrSetupNotConfigured
	}
	state, stateHash, err := newOAuthState()
	if err != nil {
		return StartSetupResult{}, err
	}
	now := time.Now().UTC()
	installationURL := s.setupURL()
	parsed, err := url.Parse(installationURL)
	if err != nil {
		return StartSetupResult{}, fmt.Errorf("%w: invalid setup URL", ErrSetupNotConfigured)
	}
	query := parsed.Query()
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	row, err := s.queries.CreateSetupSession(ctx, store.CreateSetupSessionParams{
		SetupSessionID:  pgUUID(uuid.New()),
		OrgID:           principal.OrgID,
		ActorID:         principal.ActorID,
		StateHash:       stateHash,
		InstallationUrl: parsed.String(),
		CallbackUrl:     strings.TrimSpace(callbackURL),
		ExpiresAt:       pgTime(now.Add(setupSessionTTL)),
		CreatedAt:       pgTime(now),
	})
	if err != nil {
		return StartSetupResult{}, err
	}
	session := setupSessionFromCreateRow(row)
	s.writeEvent(ctx, githubEvent{
		EventName:   "github.setup.session.created",
		Result:      "succeeded",
		OrgID:       principal.OrgID,
		StartedAt:   now,
		CompletedAt: time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"setup_session_id":     session.ID.String(),
			"idempotency_key_hash": sha256Hex([]byte(strings.TrimSpace(idempotencyKey))),
		}),
	})
	return StartSetupResult{Session: session, State: state}, nil
}

func (s *Service) GetSetupSession(ctx context.Context, principal Principal, setupSessionID uuid.UUID) (SetupSession, error) {
	if err := validatePrincipal(principal); err != nil {
		return SetupSession{}, err
	}
	row, err := s.queries.GetSetupSession(ctx, store.GetSetupSessionParams{
		SetupSessionID: pgUUID(setupSessionID),
		OrgID:          principal.OrgID,
	})
	if err != nil {
		return SetupSession{}, err
	}
	return setupSessionFromGetRow(row), nil
}

func (s *Service) StartUserAuthorization(ctx context.Context, principal Principal, callbackURL string) (StartOAuthResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return StartOAuthResult{}, err
	}
	if err := s.requireOAuthConfig(); err != nil {
		return StartOAuthResult{}, err
	}
	state, stateHash, err := newOAuthState()
	if err != nil {
		return StartOAuthResult{}, err
	}
	now := time.Now().UTC()
	redirectURL := firstNonEmpty(strings.TrimSpace(callbackURL), strings.TrimSpace(s.cfg.OAuthRedirectURL))
	authorizeURL, err := url.Parse(s.cfg.OAuthAuthorizeURL)
	if err != nil {
		return StartOAuthResult{}, err
	}
	query := authorizeURL.Query()
	query.Set("client_id", strings.TrimSpace(s.cfg.OAuthClientID))
	query.Set("state", state)
	query.Set("scope", "read:user")
	if redirectURL != "" {
		query.Set("redirect_uri", redirectURL)
	}
	authorizeURL.RawQuery = query.Encode()
	row, err := s.queries.CreateOAuthSession(ctx, store.CreateOAuthSessionParams{
		OauthSessionID:   pgUUID(uuid.New()),
		OrgID:            principal.OrgID,
		ActorID:          principal.ActorID,
		StateHash:        stateHash,
		AuthorizationUrl: authorizeURL.String(),
		CallbackUrl:      redirectURL,
		ExpiresAt:        pgTime(now.Add(oauthSessionTTL)),
		CreatedAt:        pgTime(now),
	})
	if err != nil {
		return StartOAuthResult{}, err
	}
	id := uuidFromPG(row.OauthSessionID)
	s.writeEvent(ctx, githubEvent{
		EventName:   "github.user_authorization.session.created",
		Result:      "succeeded",
		OrgID:       principal.OrgID,
		StartedAt:   now,
		CompletedAt: time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"oauth_session_id": id.String(),
		}),
	})
	return StartOAuthResult{
		OAuthSessionID:   id,
		AuthorizationURL: row.AuthorizationUrl,
		State:            state,
		ExpiresAt:        timeFromPG(row.ExpiresAt),
	}, nil
}

func (s *Service) CompleteUserAuthorization(ctx context.Context, principal Principal, oauthSessionID uuid.UUID, state, code string) (UserAuthorization, error) {
	if err := validatePrincipal(principal); err != nil {
		return UserAuthorization{}, err
	}
	if err := s.requireOAuthConfig(); err != nil {
		return UserAuthorization{}, err
	}
	now := time.Now().UTC()
	session, err := s.queries.GetOAuthSessionForCompletion(ctx, store.GetOAuthSessionForCompletionParams{
		OauthSessionID: pgUUID(oauthSessionID),
		OrgID:          principal.OrgID,
	})
	if err != nil {
		return UserAuthorization{}, err
	}
	if session.ActorID != principal.ActorID || session.SessionState != "pending" || session.StateHash != hashState(state) || !timeFromPG(session.ExpiresAt).After(now) {
		return UserAuthorization{}, ErrInvalidSetupState
	}
	token, scopes, err := s.exchangeOAuthCode(ctx, strings.TrimSpace(code), session.CallbackUrl)
	if err != nil {
		return UserAuthorization{}, err
	}
	user, err := s.fetchGitHubUser(ctx, token)
	if err != nil {
		return UserAuthorization{}, err
	}
	authorizationID := uuid.New()
	credentialRef := s.userTokenSecretName(authorizationID)
	if err := s.writeUserTokenSecret(ctx, credentialRef, token, authorizationID); err != nil {
		return UserAuthorization{}, err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return UserAuthorization{}, err
	}
	authRow, err := s.queries.UpsertUserAuthorization(ctx, store.UpsertUserAuthorizationParams{
		GithubUserAuthorizationID: pgUUID(authorizationID),
		OrgID:                     principal.OrgID,
		ActorID:                   principal.ActorID,
		ProviderUserID:            user.ID,
		GithubLogin:               user.Login,
		ScopesJson:                scopesJSON,
		CredentialRef:             credentialRef,
		AuthorizedAt:              pgTime(now),
	})
	if err != nil {
		return UserAuthorization{}, err
	}
	authz := userAuthorizationFromRow(authRow)
	_, err = s.queries.CompleteOAuthSession(ctx, store.CompleteOAuthSessionParams{
		GithubUserAuthorizationID: pgUUID(authz.ID),
		CompletedAt:               pgTime(now),
		OauthSessionID:            pgUUID(oauthSessionID),
		OrgID:                     principal.OrgID,
		StateHash:                 hashState(state),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return UserAuthorization{}, ErrInvalidSetupState
	}
	if err != nil {
		return UserAuthorization{}, err
	}
	s.writeEvent(ctx, githubEvent{
		EventName:   "github.user_authorization.completed",
		Result:      "succeeded",
		OrgID:       principal.OrgID,
		StartedAt:   now,
		CompletedAt: time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"user_authorization_id": authz.ID.String(),
			"github_login":          authz.GitHubLogin,
		}),
	})
	return authz, nil
}

func (s *Service) CompleteSetupSession(ctx context.Context, principal Principal, setupSessionID uuid.UUID, state string, installationID int64, userAuthorizationID uuid.UUID) (SetupSession, InstallationBinding, []RepositoryCandidate, error) {
	if err := validatePrincipal(principal); err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	now := time.Now().UTC()
	session, err := s.queries.GetSetupSession(ctx, store.GetSetupSessionParams{
		SetupSessionID: pgUUID(setupSessionID),
		OrgID:          principal.OrgID,
	})
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	if session.ActorID != principal.ActorID || session.StateHash != hashState(state) || !timeFromPG(session.ExpiresAt).After(now) {
		return SetupSession{}, InstallationBinding{}, nil, ErrInvalidSetupState
	}
	if userAuthorizationID == uuid.Nil {
		_, _ = s.queries.MarkSetupSessionAwaitingAuthorization(ctx, store.MarkSetupSessionAwaitingAuthorizationParams{
			FailureReason:  "github_user_authorization_required",
			UpdatedAt:      pgTime(now),
			SetupSessionID: pgUUID(setupSessionID),
			OrgID:          principal.OrgID,
		})
		return SetupSession{}, InstallationBinding{}, nil, ErrGitHubUserAuthorization
	}
	userAuth, err := s.queries.GetUserAuthorization(ctx, store.GetUserAuthorizationParams{
		GithubUserAuthorizationID: pgUUID(userAuthorizationID),
		OrgID:                     principal.OrgID,
		ActorID:                   principal.ActorID,
	})
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	userToken, err := s.readUserTokenSecret(ctx, userAuth.CredentialRef)
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	ok, err := s.userCanAccessInstallation(ctx, userToken, installationID)
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	if !ok {
		return SetupSession{}, InstallationBinding{}, nil, ErrGitHubInstallationForbidden
	}
	installation, err := s.fetchInstallation(ctx, installationID)
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	if err := s.persistInstallationDetails(ctx, installation, "", now); err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	bindingRow, err := s.queries.UpsertInstallationBinding(ctx, store.UpsertInstallationBindingParams{
		InstallationBindingID:  pgUUID(uuid.New()),
		OrgID:                  principal.OrgID,
		ProviderInstallationID: installation.ID,
		ProviderAccountID:      installation.Account.ID,
		SetupSessionID:         pgUUID(setupSessionID),
		ConnectedByActorID:     principal.ActorID,
		ConnectedAt:            pgTime(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupSession{}, InstallationBinding{}, nil, ErrInstallationAlreadyBound
	}
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	binding, err := s.installationBindingFromID(ctx, principal.OrgID, uuidFromPG(bindingRow.InstallationBindingID))
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	repositories, err := s.SyncInstallationRepositories(ctx, principal, binding.ID)
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	completed, err := s.queries.CompleteSetupSession(ctx, store.CompleteSetupSessionParams{
		ProviderInstallationID:    installation.ID,
		GithubUserAuthorizationID: pgUUID(userAuthorizationID),
		InstallationBindingID:     pgUUID(binding.ID),
		CompletedAt:               pgTime(now),
		SetupSessionID:            pgUUID(setupSessionID),
		OrgID:                     principal.OrgID,
		StateHash:                 hashState(state),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupSession{}, InstallationBinding{}, nil, ErrInvalidSetupState
	}
	if err != nil {
		return SetupSession{}, InstallationBinding{}, nil, err
	}
	sessionOut := setupSessionFromCompleteRow(completed)
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.setup.completed",
		Result:                 "succeeded",
		OrgID:                  principal.OrgID,
		InstallationBindingID:  binding.ID,
		ProviderInstallationID: uint64FromInt64(installationID),
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"setup_session_id":       setupSessionID.String(),
			"user_authorization_id":  userAuthorizationID.String(),
			"repository_count":       strconv.Itoa(len(repositories)),
			"provider_account_login": installation.Account.Login,
		}),
	})
	return sessionOut, binding, repositories, nil
}

func (s *Service) ListInstallations(ctx context.Context, principal Principal, limit int, pageToken string) (InstallationListPage, error) {
	if err := validatePrincipal(principal); err != nil {
		return InstallationListPage{}, err
	}
	pageSize := normalizePageSize(limit, 100, 500)
	offset, err := decodeOffsetPageToken(pageToken)
	if err != nil {
		return InstallationListPage{}, err
	}
	rows, err := s.queries.ListInstallationBindings(ctx, store.ListInstallationBindingsParams{
		OrgID:       principal.OrgID,
		LimitCount:  pageSize + 1,
		OffsetCount: offset,
	})
	if err != nil {
		return InstallationListPage{}, err
	}
	next := ""
	if len(rows) > int(pageSize) {
		rows = rows[:pageSize]
		next = encodeOffsetPageToken(offset + pageSize)
	}
	out := make([]InstallationBinding, 0, len(rows))
	for _, row := range rows {
		out = append(out, installationBindingFromListRow(row))
	}
	return InstallationListPage{Installations: out, NextPageToken: next}, nil
}

func (s *Service) GetInstallation(ctx context.Context, principal Principal, installationBindingID uuid.UUID) (InstallationBinding, error) {
	if err := validatePrincipal(principal); err != nil {
		return InstallationBinding{}, err
	}
	return s.installationBindingFromID(ctx, principal.OrgID, installationBindingID)
}

func (s *Service) DisconnectInstallation(ctx context.Context, principal Principal, installationBindingID uuid.UUID) (InstallationBinding, error) {
	if err := validatePrincipal(principal); err != nil {
		return InstallationBinding{}, err
	}
	now := time.Now().UTC()
	row, err := s.queries.DisconnectInstallationBinding(ctx, store.DisconnectInstallationBindingParams{
		ActorID:               principal.ActorID,
		DisconnectedAt:        pgTime(now),
		InstallationBindingID: pgUUID(installationBindingID),
		OrgID:                 principal.OrgID,
	})
	if err != nil {
		return InstallationBinding{}, err
	}
	_ = s.queries.MarkRepositoryBindingsUnavailableForInstallation(ctx, store.MarkRepositoryBindingsUnavailableForInstallationParams{
		UpdatedAt:             pgTime(now),
		InstallationBindingID: pgUUID(installationBindingID),
	})
	out := installationBindingFromDisconnectRow(row)
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.installation.disconnected",
		Result:                 "succeeded",
		OrgID:                  principal.OrgID,
		InstallationBindingID:  installationBindingID,
		ProviderInstallationID: uint64FromInt64(out.ProviderInstallationID),
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
	})
	return out, nil
}

func (s *Service) SyncInstallationRepositories(ctx context.Context, principal Principal, installationBindingID uuid.UUID) ([]RepositoryCandidate, error) {
	if err := validatePrincipal(principal); err != nil {
		return nil, err
	}
	binding, err := s.installationBindingFromID(ctx, principal.OrgID, installationBindingID)
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	reconciliationID := uuid.New()
	_ = s.queries.InsertProviderReconciliation(ctx, store.InsertProviderReconciliationParams{
		ReconciliationID:       pgUUID(reconciliationID),
		OrgID:                  principal.OrgID,
		InstallationBindingID:  pgUUID(installationBindingID),
		ProviderInstallationID: binding.ProviderInstallationID,
		Reason:                 "api_sync",
		StartedAt:              pgTime(started),
	})
	completeReconciliation := func(state string, failure error) {
		reason := ""
		if failure != nil {
			reason = truncate(failure.Error(), 1024)
		}
		_ = s.queries.CompleteProviderReconciliation(context.Background(), store.CompleteProviderReconciliationParams{
			State:            state,
			FailureReason:    reason,
			CompletedAt:      pgTime(time.Now().UTC()),
			ReconciliationID: pgUUID(reconciliationID),
		})
	}
	installation, err := s.fetchInstallation(ctx, binding.ProviderInstallationID)
	if err != nil {
		completeReconciliation("failed", err)
		return nil, err
	}
	if err := s.persistInstallationDetails(ctx, installation, "", time.Now().UTC()); err != nil {
		completeReconciliation("failed", err)
		return nil, err
	}
	repos, err := s.listInstallationRepositories(ctx, binding.ProviderInstallationID)
	if err != nil {
		completeReconciliation("failed", err)
		return nil, err
	}
	for _, repo := range repos {
		if err := s.persistRepositoryDetails(ctx, binding.ProviderInstallationID, repo, time.Now().UTC()); err != nil {
			completeReconciliation("failed", err)
			return nil, err
		}
	}
	rows, err := s.queries.ListRepositoryCandidates(ctx, store.ListRepositoryCandidatesParams{
		InstallationBindingID: pgUUID(installationBindingID),
		OrgID:                 principal.OrgID,
		LimitCount:            500,
	})
	if err != nil {
		completeReconciliation("failed", err)
		return nil, err
	}
	out := make([]RepositoryCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, repositoryCandidateFromRow(row))
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.installation.synced",
		Result:                 "succeeded",
		OrgID:                  principal.OrgID,
		InstallationBindingID:  installationBindingID,
		ProviderInstallationID: uint64FromInt64(binding.ProviderInstallationID),
		StartedAt:              started,
		CompletedAt:            time.Now().UTC(),
		AttributesJSON: mustJSON(map[string]string{
			"repository_count": strconv.Itoa(len(out)),
		}),
	})
	completeReconciliation("succeeded", nil)
	return out, nil
}

func (s *Service) ListRepositories(ctx context.Context, principal Principal, installationBindingID uuid.UUID, limit int, pageToken string) (RepositoryListPage, error) {
	if err := validatePrincipal(principal); err != nil {
		return RepositoryListPage{}, err
	}
	if installationBindingID == uuid.Nil {
		installations, err := s.ListInstallations(ctx, principal, 1, "")
		if err != nil {
			return RepositoryListPage{}, err
		}
		if len(installations.Installations) == 0 {
			return RepositoryListPage{}, nil
		}
		installationBindingID = installations.Installations[0].ID
	}
	pageSize := normalizePageSize(limit, 500, 500)
	offset, err := decodeOffsetPageToken(pageToken)
	if err != nil {
		return RepositoryListPage{}, err
	}
	rows, err := s.queries.ListRepositoryCandidates(ctx, store.ListRepositoryCandidatesParams{
		InstallationBindingID: pgUUID(installationBindingID),
		OrgID:                 principal.OrgID,
		LimitCount:            pageSize + 1,
		OffsetCount:           offset,
	})
	if err != nil {
		return RepositoryListPage{}, err
	}
	next := ""
	if len(rows) > int(pageSize) {
		rows = rows[:pageSize]
		next = encodeOffsetPageToken(offset + pageSize)
	}
	out := make([]RepositoryCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, repositoryCandidateFromRow(row))
	}
	return RepositoryListPage{Repositories: out, NextPageToken: next}, nil
}

func (s *Service) EnableRepository(ctx context.Context, principal Principal, installationBindingID uuid.UUID, repositoryID int64) (RepositoryBinding, error) {
	if err := validatePrincipal(principal); err != nil {
		return RepositoryBinding{}, err
	}
	now := time.Now().UTC()
	row, err := s.queries.EnableRepositoryBinding(ctx, store.EnableRepositoryBindingParams{
		RepositoryBindingID:   pgUUID(uuid.New()),
		ActorID:               principal.ActorID,
		EnabledAt:             pgTime(now),
		InstallationBindingID: pgUUID(installationBindingID),
		OrgID:                 principal.OrgID,
		ProviderRepositoryID:  repositoryID,
	})
	if err != nil {
		return RepositoryBinding{}, err
	}
	out, err := s.repositoryBindingFromID(ctx, principal.OrgID, uuidFromPG(row.RepositoryBindingID))
	if err != nil {
		return RepositoryBinding{}, err
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.repository.enabled",
		Result:                 "succeeded",
		OrgID:                  principal.OrgID,
		InstallationBindingID:  installationBindingID,
		RepositoryBindingID:    out.ID,
		ProviderInstallationID: uint64FromInt64(out.ProviderInstallationID),
		ProviderRepositoryID:   uint64FromInt64(out.ProviderRepositoryID),
		RepositoryFullName:     out.RepositoryFullName,
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
	})
	return out, nil
}

func (s *Service) GetRepository(ctx context.Context, principal Principal, repositoryBindingID uuid.UUID) (RepositoryBinding, error) {
	if err := validatePrincipal(principal); err != nil {
		return RepositoryBinding{}, err
	}
	return s.repositoryBindingFromID(ctx, principal.OrgID, repositoryBindingID)
}

func (s *Service) DisableRepository(ctx context.Context, principal Principal, repositoryBindingID uuid.UUID) (RepositoryBinding, error) {
	if err := validatePrincipal(principal); err != nil {
		return RepositoryBinding{}, err
	}
	now := time.Now().UTC()
	row, err := s.queries.DisableRepositoryBinding(ctx, store.DisableRepositoryBindingParams{
		ActorID:             principal.ActorID,
		DisabledAt:          pgTime(now),
		RepositoryBindingID: pgUUID(repositoryBindingID),
		OrgID:               principal.OrgID,
	})
	if err != nil {
		return RepositoryBinding{}, err
	}
	out, err := s.repositoryBindingFromID(ctx, principal.OrgID, uuidFromPG(row.RepositoryBindingID))
	if err != nil {
		return RepositoryBinding{}, err
	}
	s.writeEvent(ctx, githubEvent{
		EventName:              "github.repository.disabled",
		Result:                 "succeeded",
		OrgID:                  principal.OrgID,
		InstallationBindingID:  out.InstallationBindingID,
		RepositoryBindingID:    out.ID,
		ProviderInstallationID: uint64FromInt64(out.ProviderInstallationID),
		ProviderRepositoryID:   uint64FromInt64(out.ProviderRepositoryID),
		RepositoryFullName:     out.RepositoryFullName,
		StartedAt:              now,
		CompletedAt:            time.Now().UTC(),
	})
	return out, nil
}

func (s *Service) setupURL() string {
	if strings.TrimSpace(s.cfg.AppSetupURL) != "" {
		return strings.TrimSpace(s.cfg.AppSetupURL)
	}
	if strings.TrimSpace(s.cfg.AppSlug) == "" {
		return ""
	}
	return "https://github.com/apps/" + url.PathEscape(strings.TrimSpace(s.cfg.AppSlug)) + "/installations/new"
}

func (s *Service) requireOAuthConfig() error {
	if strings.TrimSpace(s.cfg.OAuthClientID) == "" || strings.TrimSpace(s.cfg.OAuthClientSecret) == "" {
		return ErrOAuthNotConfigured
	}
	return nil
}

func (s *Service) fetchInstallation(ctx context.Context, installationID int64) (githubInstallationResponse, error) {
	appJWT, err := createAppJWT(s.cfg.AppID, s.privateKey)
	if err != nil {
		return githubInstallationResponse{}, err
	}
	var out githubInstallationResponse
	if err := s.githubRequest(ctx, installationID, http.MethodGet, fmt.Sprintf("/app/installations/%d", installationID), appJWT, nil, &out, http.StatusOK); err != nil {
		return githubInstallationResponse{}, err
	}
	return out, nil
}

func (s *Service) listInstallationRepositories(ctx context.Context, installationID int64) ([]githubRepositoryResponse, error) {
	token, err := s.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	const perPage = 100
	var out []githubRepositoryResponse
	for page := 1; ; page++ {
		var resp githubInstallationRepositoriesResponse
		path := fmt.Sprintf("/installation/repositories?per_page=%d&page=%d", perPage, page)
		if err := s.githubRequest(ctx, installationID, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
			return nil, err
		}
		out = append(out, resp.Repositories...)
		if len(resp.Repositories) < perPage || page*perPage >= resp.TotalCount {
			break
		}
	}
	return out, nil
}

func (s *Service) userCanAccessInstallation(ctx context.Context, userToken string, installationID int64) (bool, error) {
	const perPage = 100
	for page := 1; ; page++ {
		var resp githubUserInstallationsResponse
		path := fmt.Sprintf("/user/installations?per_page=%d&page=%d", perPage, page)
		if err := s.githubUserRequest(ctx, http.MethodGet, path, userToken, nil, &resp, http.StatusOK); err != nil {
			return false, err
		}
		for _, installation := range resp.Installations {
			if installation.ID == installationID {
				return true, nil
			}
		}
		if len(resp.Installations) < perPage || page*perPage >= resp.TotalCount {
			break
		}
	}
	return false, nil
}

func (s *Service) exchangeOAuthCode(ctx context.Context, code, redirectURL string) (string, []string, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(s.cfg.OAuthClientID))
	form.Set("client_secret", strings.TrimSpace(s.cfg.OAuthClientSecret))
	form.Set("code", strings.TrimSpace(code))
	if strings.TrimSpace(redirectURL) != "" {
		form.Set("redirect_uri", strings.TrimSpace(redirectURL))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.OAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "verself-github-integration-service")
	started := time.Now().UTC()
	resp, err := s.client.Do(req)
	if err != nil {
		s.writeEvent(ctx, githubEvent{EventName: "github.oauth.exchange", Result: "failed", Reason: truncate(err.Error(), 1024), StartedAt: started, CompletedAt: time.Now().UTC()})
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("github oauth exchange: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		s.writeEvent(ctx, githubEvent{EventName: "github.oauth.exchange", Result: "failed", Reason: truncate(err.Error(), 1024), StartedAt: started, CompletedAt: time.Now().UTC()})
		return "", nil, err
	}
	var out githubOAuthTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, err
	}
	if out.Error != "" {
		return "", nil, fmt.Errorf("github oauth exchange: %s: %s", out.Error, out.Description)
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return "", nil, fmt.Errorf("github oauth exchange returned empty access_token")
	}
	s.writeEvent(ctx, githubEvent{EventName: "github.oauth.exchange", Result: "succeeded", StartedAt: started, CompletedAt: time.Now().UTC()})
	return out.AccessToken, splitScopes(out.Scope), nil
}

func (s *Service) fetchGitHubUser(ctx context.Context, userToken string) (githubUserResponse, error) {
	var out githubUserResponse
	if err := s.githubUserRequest(ctx, http.MethodGet, "/user", userToken, nil, &out, http.StatusOK); err != nil {
		return githubUserResponse{}, err
	}
	if out.ID <= 0 || strings.TrimSpace(out.Login) == "" {
		return githubUserResponse{}, fmt.Errorf("github user response missing id/login")
	}
	return out, nil
}

func (s *Service) githubUserRequest(ctx context.Context, method, path, bearer string, body any, out any, expected ...int) error {
	started := time.Now().UTC()
	apiBase := strings.TrimRight(s.cfg.APIBaseURL, "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "verself-github-integration-service")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.writeGitHubAPIEvent(ctx, 0, method, path, 0, "", "", started, err)
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	for _, status := range expected {
		if resp.StatusCode == status {
			s.writeGitHubAPIEvent(ctx, 0, method, path, resp.StatusCode, resp.Header.Get("x-ratelimit-remaining"), resp.Header.Get("x-ratelimit-reset"), started, nil)
			if out != nil {
				return json.NewDecoder(resp.Body).Decode(out)
			}
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	err = fmt.Errorf("github api %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(detail)))
	s.writeGitHubAPIEvent(ctx, 0, method, path, resp.StatusCode, resp.Header.Get("x-ratelimit-remaining"), resp.Header.Get("x-ratelimit-reset"), started, err)
	return err
}

func (s *Service) persistInstallationDetails(ctx context.Context, installation githubInstallationResponse, deliveryID string, now time.Time) error {
	return s.persistInstallationDetailsWithState(ctx, installation, deliveryID, now, "")
}

func (s *Service) persistInstallationDetailsWithState(ctx context.Context, installation githubInstallationResponse, deliveryID string, now time.Time, observedState string) error {
	if installation.Account.ID > 0 {
		if err := s.queries.UpsertGithubAccount(ctx, store.UpsertGithubAccountParams{
			ProviderAccountID:   installation.Account.ID,
			Login:               strings.TrimSpace(installation.Account.Login),
			AccountType:         firstNonEmpty(installation.Account.Type, installation.TargetType, "unknown"),
			AvatarUrl:           strings.TrimSpace(installation.Account.AvatarURL),
			HtmlUrl:             strings.TrimSpace(installation.Account.HTMLURL),
			State:               "active",
			LastEventDeliveryID: deliveryID,
			ObservedFromApiAt:   pgTime(now),
			UpdatedAt:           pgTime(now),
		}); err != nil {
			return err
		}
	}
	state := strings.TrimSpace(observedState)
	if state == "" {
		state = "active"
	}
	if installation.SuspendedAt != nil {
		state = "suspended"
	}
	permissions := installation.Permissions
	if len(permissions) == 0 {
		permissions = []byte(`{}`)
	}
	return s.queries.UpsertInstallationDetails(ctx, store.UpsertInstallationDetailsParams{
		ProviderInstallationID: installation.ID,
		ProviderAccountID:      installation.Account.ID,
		AccountLogin:           strings.TrimSpace(installation.Account.Login),
		AccountType:            firstNonEmpty(installation.Account.Type, installation.TargetType, "unknown"),
		AppSlug:                strings.TrimSpace(installation.AppSlug),
		TargetType:             strings.TrimSpace(installation.TargetType),
		RepositorySelection:    strings.TrimSpace(installation.RepositorySelection),
		ConfigurationUrl:       strings.TrimSpace(installation.HTMLURL),
		PermissionsJson:        permissions,
		State:                  state,
		LastEventDeliveryID:    deliveryID,
		ObservedFromApiAt:      pgTime(now),
		UpdatedAt:              pgTime(now),
	})
}

func (s *Service) persistRepositoryDetails(ctx context.Context, installationID int64, repo githubRepositoryResponse, now time.Time) error {
	return s.persistRepositoryDetailsWithState(ctx, installationID, repo, now, "")
}

func (s *Service) persistRepositoryDetailsWithState(ctx context.Context, installationID int64, repo githubRepositoryResponse, now time.Time, observedState string) error {
	ownerLogin := strings.TrimSpace(repo.Owner.Login)
	if ownerLogin == "" {
		ownerLogin, _, _ = strings.Cut(repo.FullName, "/")
	}
	if err := s.queries.UpsertGithubAccount(ctx, store.UpsertGithubAccountParams{
		ProviderAccountID: repo.Owner.ID,
		Login:             ownerLogin,
		AccountType:       firstNonEmpty(repo.Owner.Type, "unknown"),
		AvatarUrl:         strings.TrimSpace(repo.Owner.AvatarURL),
		HtmlUrl:           strings.TrimSpace(repo.Owner.HTMLURL),
		State:             "active",
		ObservedFromApiAt: pgTime(now),
		UpdatedAt:         pgTime(now),
	}); err != nil && repo.Owner.ID > 0 {
		return err
	}
	state := strings.TrimSpace(observedState)
	if state == "" {
		state = "active"
		if repo.Archived {
			state = "archived"
		}
	}
	if err := s.queries.UpsertRepositoryDetails(ctx, store.UpsertRepositoryDetailsParams{
		ProviderRepositoryID: repo.ID,
		ProviderAccountID:    repo.Owner.ID,
		OwnerLogin:           ownerLogin,
		RepositoryName:       strings.TrimSpace(repo.Name),
		RepositoryFullName:   strings.TrimSpace(repo.FullName),
		DefaultBranch:        strings.TrimSpace(repo.DefaultBranch),
		Private:              repo.Private,
		Archived:             repo.Archived,
		Visibility:           strings.TrimSpace(repo.Visibility),
		HtmlUrl:              strings.TrimSpace(repo.HTMLURL),
		State:                state,
		ObservedFromApiAt:    pgTime(now),
		UpdatedAt:            pgTime(now),
	}); err != nil {
		return err
	}
	if installationID <= 0 {
		return nil
	}
	return s.queries.UpsertInstallationRepository(ctx, store.UpsertInstallationRepositoryParams{
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repo.ID,
		State:                  "selected",
		ObservedFromApiAt:      pgTime(now),
		UpdatedAt:              pgTime(now),
	})
}

func (s *Service) writeUserTokenSecret(ctx context.Context, name, token string, id uuid.UUID) error {
	if s.cfg.Secrets == nil {
		return fmt.Errorf("%w: secrets client is required", ErrOAuthNotConfigured)
	}
	resp, err := s.cfg.Secrets.PutSecret(ctx, secretsclient.PutSecretRequest{
		IdempotencyKey: secretsclient.IdempotencyKey("github-user-token-" + id.String()),
		Name:           secretsclient.SecretName(name),
		Body: secretsclient.SecretMutationInputBody{
			Value: secretsclient.SecretValue(token),
		},
	})
	if err != nil {
		return err
	}
	if resp.Result == nil {
		return fmt.Errorf("store github user token: status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

func (s *Service) readUserTokenSecret(ctx context.Context, name string) (string, error) {
	if s.cfg.Secrets == nil {
		return "", fmt.Errorf("%w: secrets client is required", ErrOAuthNotConfigured)
	}
	resp, err := s.cfg.Secrets.ReadSecret(ctx, secretsclient.ReadSecretRequest{Name: secretsclient.SecretName(name)})
	if err != nil {
		return "", err
	}
	if resp.Result == nil {
		return "", fmt.Errorf("read github user token: status %d: %s", resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return string(resp.Result.Value), nil
}

func (s *Service) userTokenSecretName(id uuid.UUID) string {
	return strings.TrimSpace(s.cfg.UserTokenSecretPrefix) + id.String()
}

func (s *Service) installationBindingFromID(ctx context.Context, orgID string, id uuid.UUID) (InstallationBinding, error) {
	row, err := s.queries.GetInstallationBinding(ctx, store.GetInstallationBindingParams{
		InstallationBindingID: pgUUID(id),
		OrgID:                 orgID,
	})
	if err != nil {
		return InstallationBinding{}, err
	}
	return installationBindingFromGetRow(row), nil
}

func (s *Service) repositoryBindingFromID(ctx context.Context, orgID string, id uuid.UUID) (RepositoryBinding, error) {
	row, err := s.queries.GetRepositoryBinding(ctx, store.GetRepositoryBindingParams{
		RepositoryBindingID: pgUUID(id),
		OrgID:               orgID,
	})
	if err != nil {
		return RepositoryBinding{}, err
	}
	return repositoryBindingFromGetRow(row), nil
}

func validatePrincipal(principal Principal) error {
	if strings.TrimSpace(principal.OrgID) == "" || strings.TrimSpace(principal.ActorID) == "" {
		return fmt.Errorf("authenticated principal requires org_id and actor_id")
	}
	return nil
}

func newOAuthState() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	return state, hashState(state), nil
}

func hashState(state string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(state)))
	return hexString(sum[:])
}

func hexString(raw []byte) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, b := range raw {
		out[i*2] = alphabet[b>>4]
		out[i*2+1] = alphabet[b&0x0f]
	}
	return string(out)
}

func splitScopes(scope string) []string {
	parts := strings.Split(scope, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

type offsetPageToken struct {
	Offset int32 `json:"offset"`
}

func decodeOffsetPageToken(token string) (int32, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, ErrInvalidPageToken
	}
	var payload offsetPageToken
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Offset < 0 {
		return 0, ErrInvalidPageToken
	}
	return payload.Offset, nil
}

func encodeOffsetPageToken(offset int32) string {
	if offset <= 0 {
		return ""
	}
	payload, err := json.Marshal(offsetPageToken{Offset: offset})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func normalizePageSize(value int, fallback, max int32) int32 {
	if value <= 0 {
		return fallback
	}
	if value > int(max) {
		return max
	}
	return int32(value) // #nosec G115 -- value is clamped to max before conversion.
}

func parseScopes(payload []byte) []string {
	var scopes []string
	_ = json.Unmarshal(payload, &scopes)
	return scopes
}

func setupSessionFromCreateRow(row store.CreateSetupSessionRow) SetupSession {
	return SetupSession{
		ID:                     uuidFromPG(row.SetupSessionID),
		OrgID:                  row.OrgID,
		ActorID:                row.ActorID,
		State:                  row.SessionState,
		InstallationURL:        row.InstallationUrl,
		CallbackURL:            row.CallbackUrl,
		ProviderInstallationID: row.ProviderInstallationID,
		UserAuthorizationID:    uuidFromPG(row.GithubUserAuthorizationID),
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		ExpiresAt:              timeFromPG(row.ExpiresAt),
		CompletedAt:            timeFromPG(row.CompletedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func setupSessionFromGetRow(row store.GetSetupSessionRow) SetupSession {
	return SetupSession{
		ID:                     uuidFromPG(row.SetupSessionID),
		OrgID:                  row.OrgID,
		ActorID:                row.ActorID,
		State:                  row.SessionState,
		InstallationURL:        row.InstallationUrl,
		CallbackURL:            row.CallbackUrl,
		ProviderInstallationID: row.ProviderInstallationID,
		UserAuthorizationID:    uuidFromPG(row.GithubUserAuthorizationID),
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		ExpiresAt:              timeFromPG(row.ExpiresAt),
		CompletedAt:            timeFromPG(row.CompletedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func setupSessionFromCompleteRow(row store.CompleteSetupSessionRow) SetupSession {
	return SetupSession{
		ID:                     uuidFromPG(row.SetupSessionID),
		OrgID:                  row.OrgID,
		ActorID:                row.ActorID,
		State:                  row.SessionState,
		InstallationURL:        row.InstallationUrl,
		CallbackURL:            row.CallbackUrl,
		ProviderInstallationID: row.ProviderInstallationID,
		UserAuthorizationID:    uuidFromPG(row.GithubUserAuthorizationID),
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		ExpiresAt:              timeFromPG(row.ExpiresAt),
		CompletedAt:            timeFromPG(row.CompletedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func userAuthorizationFromRow(row store.GithubUserAuthorization) UserAuthorization {
	return UserAuthorization{
		ID:             uuidFromPG(row.GithubUserAuthorizationID),
		OrgID:          row.OrgID,
		ActorID:        row.ActorID,
		ProviderUserID: row.ProviderUserID,
		GitHubLogin:    row.GithubLogin,
		Scopes:         parseScopes(row.ScopesJson),
		CredentialRef:  row.CredentialRef,
		State:          row.State,
		AuthorizedAt:   timeFromPG(row.AuthorizedAt),
		LastVerifiedAt: timeFromPG(row.LastVerifiedAt),
		RevokedAt:      timeFromPG(row.RevokedAt),
		CreatedAt:      timeFromPG(row.CreatedAt),
		UpdatedAt:      timeFromPG(row.UpdatedAt),
	}
}

func installationBindingFromGetRow(row store.GetInstallationBindingRow) InstallationBinding {
	return InstallationBinding{
		ID:                     uuidFromPG(row.InstallationBindingID),
		OrgID:                  row.OrgID,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderAccountID:      row.ProviderAccountID,
		AccountLogin:           row.AccountLogin,
		AccountType:            row.AccountType,
		AppSlug:                row.AppSlug,
		RepositorySelection:    row.RepositorySelection,
		ConfigurationURL:       row.ConfigurationUrl,
		SetupSessionID:         uuidFromPG(row.SetupSessionID),
		ConnectedByActorID:     row.ConnectedByActorID,
		State:                  row.State,
		ConnectedAt:            timeFromPG(row.ConnectedAt),
		DisconnectedAt:         timeFromPG(row.DisconnectedAt),
		LastSyncedAt:           timeFromPG(row.LastSyncedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func installationBindingFromListRow(row store.ListInstallationBindingsRow) InstallationBinding {
	return InstallationBinding{
		ID:                     uuidFromPG(row.InstallationBindingID),
		OrgID:                  row.OrgID,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderAccountID:      row.ProviderAccountID,
		AccountLogin:           row.AccountLogin,
		AccountType:            row.AccountType,
		AppSlug:                row.AppSlug,
		RepositorySelection:    row.RepositorySelection,
		ConfigurationURL:       row.ConfigurationUrl,
		SetupSessionID:         uuidFromPG(row.SetupSessionID),
		ConnectedByActorID:     row.ConnectedByActorID,
		State:                  row.State,
		ConnectedAt:            timeFromPG(row.ConnectedAt),
		DisconnectedAt:         timeFromPG(row.DisconnectedAt),
		LastSyncedAt:           timeFromPG(row.LastSyncedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func installationBindingFromDisconnectRow(row store.DisconnectInstallationBindingRow) InstallationBinding {
	return InstallationBinding{
		ID:                     uuidFromPG(row.InstallationBindingID),
		OrgID:                  row.OrgID,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderAccountID:      row.ProviderAccountID,
		SetupSessionID:         uuidFromPG(row.SetupSessionID),
		ConnectedByActorID:     row.ConnectedByActorID,
		State:                  row.State,
		ConnectedAt:            timeFromPG(row.ConnectedAt),
		DisconnectedAt:         timeFromPG(row.DisconnectedAt),
		LastSyncedAt:           timeFromPG(row.LastSyncedAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}

func repositoryCandidateFromRow(row store.ListRepositoryCandidatesRow) RepositoryCandidate {
	return RepositoryCandidate{
		ProviderInstallationID:      row.ProviderInstallationID,
		ProviderRepositoryID:        row.ProviderRepositoryID,
		OwnerLogin:                  row.OwnerLogin,
		RepositoryName:              row.RepositoryName,
		RepositoryFullName:          row.RepositoryFullName,
		DefaultBranch:               row.DefaultBranch,
		Private:                     row.Private,
		InstallationRepositoryState: row.InstallationRepositoryState,
		RepositoryBindingID:         uuidFromPG(row.RepositoryBindingID),
		RepositoryBindingState:      row.RepositoryBindingState,
		ObservedFromAPIAt:           timeFromPG(row.ObservedFromApiAt),
	}
}

func repositoryBindingFromGetRow(row store.GetRepositoryBindingRow) RepositoryBinding {
	return RepositoryBinding{
		ID:                     uuidFromPG(row.RepositoryBindingID),
		OrgID:                  row.OrgID,
		InstallationBindingID:  uuidFromPG(row.InstallationBindingID),
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		OwnerLogin:             row.OwnerLogin,
		RepositoryName:         row.RepositoryName,
		RepositoryFullName:     row.RepositoryFullName,
		DefaultBranch:          row.DefaultBranch,
		Private:                row.Private,
		State:                  row.State,
		EnabledByActorID:       row.EnabledByActorID,
		DisabledByActorID:      row.DisabledByActorID,
		EnabledAt:              timeFromPG(row.EnabledAt),
		DisabledAt:             timeFromPG(row.DisabledAt),
		ObservedFromAPIAt:      timeFromPG(row.ObservedFromApiAt),
		CreatedAt:              timeFromPG(row.CreatedAt),
		UpdatedAt:              timeFromPG(row.UpdatedAt),
	}
}
