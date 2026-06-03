package deployment

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
	allowedRefs         map[string]struct{}
	allowedWorkflowRefs map[string]struct{}
}

type githubClaims struct {
	Repository     string `json:"repository"`
	Ref            string `json:"ref"`
	SHA            string `json:"sha"`
	RunID          string `json:"run_id"`
	RunAttempt     string `json:"run_attempt"`
	Workflow       string `json:"workflow"`
	WorkflowRef    string `json:"workflow_ref"`
	JobWorkflowRef string `json:"job_workflow_ref"`
}

func NewGitHubOIDCVerifier(ctx context.Context, issuerURL, audience, repositories, refs, workflowRefs string) (*GitHubOIDCVerifier, error) {
	if strings.TrimSpace(issuerURL) == "" {
		return nil, fmt.Errorf("github oidc issuer is required")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("github oidc audience is required")
	}
	allowedRepositories := parseCSVSet(repositories)
	if len(allowedRepositories) == 0 {
		return nil, fmt.Errorf("at least one allowed repository is required")
	}
	allowedRefs := parseCSVSet(refs)
	if len(allowedRefs) == 0 {
		return nil, fmt.Errorf("at least one allowed ref is required")
	}
	allowedWorkflowRefs := parseCSVSet(workflowRefs)
	if len(allowedWorkflowRefs) == 0 {
		return nil, fmt.Errorf("at least one allowed workflow ref is required")
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
		allowedRepositories: allowedRepositories,
		allowedRefs:         allowedRefs,
		allowedWorkflowRefs: allowedWorkflowRefs,
	}, nil
}

func (v *GitHubOIDCVerifier) SourceFromRequest(ctx context.Context, r *http.Request) (Source, error) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return Source{}, fmt.Errorf("%w: missing bearer token", ErrUnauthorized)
	}
	return v.SourceFromToken(ctx, token)
}

func (v *GitHubOIDCVerifier) SourceFromToken(ctx context.Context, token string) (Source, error) {
	if v == nil || v.verifier == nil {
		return Source{}, fmt.Errorf("%w: github oidc verifier unavailable", ErrUnauthorized)
	}
	verified, err := v.verifier.Verify(ctx, token)
	if err != nil {
		return Source{}, fmt.Errorf("%w: verify github oidc token: %v", ErrUnauthorized, err)
	}
	var claims githubClaims
	if err := verified.Claims(&claims); err != nil {
		return Source{}, fmt.Errorf("%w: decode github oidc claims: %v", ErrUnauthorized, err)
	}
	return v.sourceFromClaims(verified.Subject, claims)
}

func (v *GitHubOIDCVerifier) sourceFromClaims(subject string, claims githubClaims) (Source, error) {
	if _, ok := v.allowedRepositories[claims.Repository]; !ok {
		return Source{}, fmt.Errorf("%w: repository %q is not allowed", ErrUnauthorized, claims.Repository)
	}
	if _, ok := v.allowedRefs[claims.Ref]; !ok {
		return Source{}, fmt.Errorf("%w: ref %q is not allowed", ErrUnauthorized, claims.Ref)
	}
	workflowRef := firstNonEmpty(claims.JobWorkflowRef, claims.WorkflowRef)
	if _, ok := v.allowedWorkflowRefs[workflowRef]; !ok {
		return Source{}, fmt.Errorf("%w: workflow ref %q is not allowed", ErrUnauthorized, workflowRef)
	}
	attempt, err := parseUint32(claims.RunAttempt)
	if err != nil {
		return Source{}, fmt.Errorf("%w: github run_attempt: %v", ErrUnauthorized, err)
	}
	return Source{
		Kind:           SourceGitHubActionsOIDC,
		Subject:        subject,
		Repository:     claims.Repository,
		Ref:            claims.Ref,
		SHA:            claims.SHA,
		RunID:          claims.RunID,
		RunAttempt:     attempt,
		Workflow:       claims.Workflow,
		JobWorkflowRef: workflowRef,
	}, nil
}

func parseCSVSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out[item] = struct{}{}
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
