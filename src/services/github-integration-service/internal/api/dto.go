package api

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/verself/github-integration-service/internal/githubintegration"
)

type setupSessionDTO struct {
	SetupSessionID         string  `json:"setup_session_id"`
	ResourceName           string  `json:"resourceName"`
	OrgID                  string  `json:"org_id"`
	ActorID                string  `json:"actor_id"`
	State                  string  `json:"state"`
	InstallationURL        string  `json:"installation_url,omitempty"`
	CallbackURL            string  `json:"callback_url,omitempty"`
	ProviderInstallationID int64   `json:"provider_installation_id,omitempty"`
	UserAuthorizationID    *string `json:"user_authorization_id,omitempty"`
	InstallationBindingID  *string `json:"installation_binding_id,omitempty"`
	ExpiresAt              string  `json:"expires_at,omitempty"`
	CompletedAt            *string `json:"completed_at,omitempty"`
	CreatedAt              string  `json:"created_at,omitempty"`
	UpdatedAt              string  `json:"updated_at,omitempty"`
}

type userAuthorizationDTO struct {
	UserAuthorizationID string   `json:"user_authorization_id"`
	ResourceName        string   `json:"resourceName"`
	OrgID               string   `json:"org_id"`
	ActorID             string   `json:"actor_id"`
	ProviderUserID      int64    `json:"provider_user_id"`
	GitHubLogin         string   `json:"github_login"`
	State               string   `json:"state"`
	Scopes              []string `json:"scopes,omitempty"`
	AuthorizedAt        string   `json:"authorized_at,omitempty"`
	LastVerifiedAt      *string  `json:"last_verified_at,omitempty"`
	RevokedAt           *string  `json:"revoked_at,omitempty"`
}

type installationBindingDTO struct {
	InstallationBindingID  string  `json:"installation_binding_id"`
	ResourceName           string  `json:"resourceName"`
	OrgID                  string  `json:"org_id"`
	ProviderInstallationID int64   `json:"provider_installation_id"`
	ProviderAccountID      int64   `json:"provider_account_id"`
	AccountLogin           string  `json:"account_login"`
	AccountType            string  `json:"account_type"`
	AppSlug                string  `json:"app_slug,omitempty"`
	RepositorySelection    string  `json:"repository_selection"`
	ConfigurationURL       string  `json:"configuration_url,omitempty"`
	SetupSessionID         *string `json:"setup_session_id,omitempty"`
	ConnectedByActorID     string  `json:"connected_by_actor_id"`
	State                  string  `json:"state"`
	ConnectedAt            string  `json:"connected_at,omitempty"`
	DisconnectedAt         *string `json:"disconnected_at,omitempty"`
	LastSyncedAt           *string `json:"last_synced_at,omitempty"`
	CreatedAt              string  `json:"created_at,omitempty"`
	UpdatedAt              string  `json:"updated_at,omitempty"`
}

type repositoryBindingDTO struct {
	RepositoryBindingID    string  `json:"repository_binding_id"`
	ResourceName           string  `json:"resourceName"`
	OrgID                  string  `json:"org_id"`
	InstallationBindingID  string  `json:"installation_binding_id"`
	ProviderInstallationID int64   `json:"provider_installation_id"`
	ProviderRepositoryID   int64   `json:"provider_repository_id"`
	RepositoryFullName     string  `json:"repository_full_name"`
	OwnerLogin             string  `json:"owner_login"`
	RepositoryName         string  `json:"repository_name"`
	State                  string  `json:"state"`
	DefaultBranch          string  `json:"default_branch,omitempty"`
	Private                bool    `json:"private"`
	EnabledByActorID       string  `json:"enabled_by_actor_id,omitempty"`
	EnabledAt              *string `json:"enabled_at,omitempty"`
	DisabledAt             *string `json:"disabled_at,omitempty"`
	ObservedFromAPIAt      *string `json:"observed_from_api_at,omitempty"`
}

type repositoryCandidateDTO struct {
	ProviderInstallationID      int64   `json:"provider_installation_id"`
	ProviderRepositoryID        int64   `json:"provider_repository_id"`
	RepositoryFullName          string  `json:"repository_full_name"`
	OwnerLogin                  string  `json:"owner_login"`
	RepositoryName              string  `json:"repository_name"`
	InstallationRepositoryState string  `json:"installation_repository_state"`
	RepositoryBindingID         *string `json:"repository_binding_id,omitempty"`
	RepositoryBindingState      string  `json:"repository_binding_state,omitempty"`
	DefaultBranch               string  `json:"default_branch,omitempty"`
	Private                     bool    `json:"private"`
	ObservedFromAPIAt           *string `json:"observed_from_api_at,omitempty"`
}

func setupSessionDTOFromDomain(in githubintegration.SetupSession) setupSessionDTO {
	return setupSessionDTO{
		SetupSessionID:         in.ID.String(),
		ResourceName:           resourceName(in.OrgID, "githubSetupSessions", in.ID),
		OrgID:                  in.OrgID,
		ActorID:                in.ActorID,
		State:                  in.State,
		InstallationURL:        in.InstallationURL,
		CallbackURL:            in.CallbackURL,
		ProviderInstallationID: in.ProviderInstallationID,
		UserAuthorizationID:    optionalUUID(in.UserAuthorizationID),
		InstallationBindingID:  optionalUUID(in.InstallationBindingID),
		ExpiresAt:              formatTime(in.ExpiresAt),
		CompletedAt:            optionalTime(in.CompletedAt),
		CreatedAt:              formatTime(in.CreatedAt),
		UpdatedAt:              formatTime(in.UpdatedAt),
	}
}

func userAuthorizationDTOFromDomain(in githubintegration.UserAuthorization) userAuthorizationDTO {
	return userAuthorizationDTO{
		UserAuthorizationID: in.ID.String(),
		ResourceName:        resourceName(in.OrgID, "githubUserAuthorizations", in.ID),
		OrgID:               in.OrgID,
		ActorID:             in.ActorID,
		ProviderUserID:      in.ProviderUserID,
		GitHubLogin:         in.GitHubLogin,
		State:               in.State,
		Scopes:              in.Scopes,
		AuthorizedAt:        formatTime(in.AuthorizedAt),
		LastVerifiedAt:      optionalTime(in.LastVerifiedAt),
		RevokedAt:           optionalTime(in.RevokedAt),
	}
}

func installationDTOFromDomain(in githubintegration.InstallationBinding) installationBindingDTO {
	return installationBindingDTO{
		InstallationBindingID:  in.ID.String(),
		ResourceName:           resourceName(in.OrgID, "githubInstallations", in.ID),
		OrgID:                  in.OrgID,
		ProviderInstallationID: in.ProviderInstallationID,
		ProviderAccountID:      in.ProviderAccountID,
		AccountLogin:           in.AccountLogin,
		AccountType:            in.AccountType,
		AppSlug:                in.AppSlug,
		RepositorySelection:    in.RepositorySelection,
		ConfigurationURL:       in.ConfigurationURL,
		SetupSessionID:         optionalUUID(in.SetupSessionID),
		ConnectedByActorID:     in.ConnectedByActorID,
		State:                  in.State,
		ConnectedAt:            formatTime(in.ConnectedAt),
		DisconnectedAt:         optionalTime(in.DisconnectedAt),
		LastSyncedAt:           optionalTime(in.LastSyncedAt),
		CreatedAt:              formatTime(in.CreatedAt),
		UpdatedAt:              formatTime(in.UpdatedAt),
	}
}

func repositoryDTOFromDomain(in githubintegration.RepositoryBinding) repositoryBindingDTO {
	return repositoryBindingDTO{
		RepositoryBindingID:    in.ID.String(),
		ResourceName:           resourceName(in.OrgID, "githubRepositories", in.ID),
		OrgID:                  in.OrgID,
		InstallationBindingID:  in.InstallationBindingID.String(),
		ProviderInstallationID: in.ProviderInstallationID,
		ProviderRepositoryID:   in.ProviderRepositoryID,
		RepositoryFullName:     in.RepositoryFullName,
		OwnerLogin:             in.OwnerLogin,
		RepositoryName:         in.RepositoryName,
		State:                  in.State,
		DefaultBranch:          in.DefaultBranch,
		Private:                in.Private,
		EnabledByActorID:       in.EnabledByActorID,
		EnabledAt:              optionalTime(in.EnabledAt),
		DisabledAt:             optionalTime(in.DisabledAt),
		ObservedFromAPIAt:      optionalTime(in.ObservedFromAPIAt),
	}
}

func repositoryCandidateDTOFromDomain(in githubintegration.RepositoryCandidate) repositoryCandidateDTO {
	return repositoryCandidateDTO{
		ProviderInstallationID:      in.ProviderInstallationID,
		ProviderRepositoryID:        in.ProviderRepositoryID,
		RepositoryFullName:          in.RepositoryFullName,
		OwnerLogin:                  in.OwnerLogin,
		RepositoryName:              in.RepositoryName,
		InstallationRepositoryState: in.InstallationRepositoryState,
		RepositoryBindingID:         optionalUUID(in.RepositoryBindingID),
		RepositoryBindingState:      in.RepositoryBindingState,
		DefaultBranch:               in.DefaultBranch,
		Private:                     in.Private,
		ObservedFromAPIAt:           optionalTime(in.ObservedFromAPIAt),
	}
}

func installationDTOs(in []githubintegration.InstallationBinding) []installationBindingDTO {
	out := make([]installationBindingDTO, 0, len(in))
	for _, item := range in {
		out = append(out, installationDTOFromDomain(item))
	}
	return out
}

func repositoryCandidateDTOs(in []githubintegration.RepositoryCandidate) []repositoryCandidateDTO {
	out := make([]repositoryCandidateDTO, 0, len(in))
	for _, item := range in {
		out = append(out, repositoryCandidateDTOFromDomain(item))
	}
	return out
}

func resourceName(orgID, collection string, id uuid.UUID) string {
	return fmt.Sprintf("urn:verself:github-integration:orgs/%s/%s/%s", orgID, collection, id.String())
}

func optionalUUID(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	text := id.String()
	return &text
}

func optionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	text := formatTime(t)
	return &text
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
