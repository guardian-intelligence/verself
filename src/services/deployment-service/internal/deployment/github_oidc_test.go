package deployment

import "testing"

func TestGitHubOIDCSourceRequiresAllowedWorkflowRef(t *testing.T) {
	verifier := GitHubOIDCVerifier{
		allowedRepositories: map[string]struct{}{"guardian-intelligence/verself": {}},
		allowedRefs:         map[string]struct{}{"refs/heads/main": {}},
		allowedWorkflowRefs: map[string]struct{}{"guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main": {}},
	}
	_, err := verifier.sourceFromClaims("repo:guardian-intelligence/verself:ref:refs/heads/main", githubClaims{
		Repository:     "guardian-intelligence/verself",
		Ref:            "refs/heads/main",
		SHA:            "0123456789abcdef0123456789abcdef01234567",
		RunAttempt:     "1",
		Workflow:       "Gamma Deploy",
		JobWorkflowRef: "guardian-intelligence/verself/.github/workflows/other.yml@refs/heads/main",
	})
	if err == nil {
		t.Fatal("unexpectedly accepted a GitHub OIDC token from an unapproved workflow")
	}
	if ErrorCode(err) != "deployment.unauthorized" {
		t.Fatalf("error code = %q, want deployment.unauthorized", ErrorCode(err))
	}
}

func TestGitHubOIDCSourceAcceptsAllowedWorkflowRef(t *testing.T) {
	verifier := GitHubOIDCVerifier{
		allowedRepositories: map[string]struct{}{"guardian-intelligence/verself": {}},
		allowedRefs:         map[string]struct{}{"refs/heads/main": {}},
		allowedWorkflowRefs: map[string]struct{}{"guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main": {}},
	}
	source, err := verifier.sourceFromClaims("repo:guardian-intelligence/verself:ref:refs/heads/main", githubClaims{
		Repository:  "guardian-intelligence/verself",
		Ref:         "refs/heads/main",
		SHA:         "0123456789abcdef0123456789abcdef01234567",
		RunAttempt:  "2",
		Workflow:    "Gamma Deploy",
		WorkflowRef: "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.JobWorkflowRef != "guardian-intelligence/verself/.github/workflows/gamma-deploy.yml@refs/heads/main" {
		t.Fatalf("workflow ref = %q", source.JobWorkflowRef)
	}
	if source.RunAttempt != 2 {
		t.Fatalf("run attempt = %d, want 2", source.RunAttempt)
	}
}
