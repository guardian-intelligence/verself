package analytics

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type GitHubOIDCVerifier struct {
	verifier            *oidc.IDTokenVerifier
	allowedRepositories map[string]struct{}
}

type githubClaims struct {
	Repository      string `json:"repository"`
	RepositoryOwner string `json:"repository_owner"`
	Ref             string `json:"ref"`
	SHA             string `json:"sha"`
	RunID           string `json:"run_id"`
	RunAttempt      string `json:"run_attempt"`
	Workflow        string `json:"workflow"`
	JobWorkflowRef  string `json:"job_workflow_ref"`
}

func NewGitHubOIDCVerifier(ctx context.Context, issuerURL, audience, allowedRepositories string) (*GitHubOIDCVerifier, error) {
	if strings.TrimSpace(issuerURL) == "" {
		return nil, fmt.Errorf("github oidc issuer is required")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("github oidc audience is required")
	}
	allowed := parseAllowedRepositories(allowedRepositories)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("at least one allowed repository is required")
	}
	providerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(providerCtx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("github oidc discovery: %w", err)
	}
	return &GitHubOIDCVerifier{
		verifier: provider.Verifier(&oidc.Config{
			ClientID: audience,
		}),
		allowedRepositories: allowed,
	}, nil
}

func (v *GitHubOIDCVerifier) SourceFromRequest(ctx context.Context, r *http.Request) (Source, error) {
	if v == nil || v.verifier == nil {
		return Source{}, fmt.Errorf("github oidc verifier unavailable")
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Source{}, fmt.Errorf("missing bearer token")
	}
	verified, err := v.verifier.Verify(ctx, token)
	if err != nil {
		return Source{}, fmt.Errorf("verify github oidc token: %w", err)
	}
	var claims githubClaims
	if err := verified.Claims(&claims); err != nil {
		return Source{}, fmt.Errorf("decode github oidc claims: %w", err)
	}
	if _, ok := v.allowedRepositories[claims.Repository]; !ok {
		return Source{}, fmt.Errorf("repository %q is not allowed", claims.Repository)
	}
	attempt, err := parseUint32(claims.RunAttempt)
	if err != nil {
		return Source{}, fmt.Errorf("github oidc run_attempt: %w", err)
	}
	return Source{
		Kind:            SourceKindGitHubActionsOIDC,
		Subject:         verified.Subject,
		TenantID:        "github:" + claims.RepositoryOwner,
		Repository:      claims.Repository,
		RepositoryOwner: claims.RepositoryOwner,
		Ref:             claims.Ref,
		SHA:             claims.SHA,
		RunID:           claims.RunID,
		RunAttempt:      attempt,
		Workflow:        claims.Workflow,
		Job:             claims.JobWorkflowRef,
	}, nil
}

func parseAllowedRepositories(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = struct{}{}
		}
	}
	return out
}

func bearerToken(header string) string {
	prefix, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func parseUint32(raw string) (uint32, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(value), nil
}
