package main

import (
	"context"
	"testing"
)

func TestGitHubVerifierDisabledWithoutAllowLists(t *testing.T) {
	verifier, err := githubVerifier(context.Background(), "https://deployments.api.gamma.verself.sh", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if verifier != nil {
		t.Fatal("expected GitHub verifier to be disabled")
	}
}

func TestGitHubVerifierRequiresWorkflowRefsWhenEnabled(t *testing.T) {
	_, err := githubVerifier(
		context.Background(),
		"https://deployments.api.gamma.verself.sh",
		"guardian-intelligence/verself",
		"refs/heads/main",
		"",
	)
	if err == nil {
		t.Fatal("expected missing workflow refs to fail")
	}
}

func TestDeploymentAuthConfigurationRequiresGitHubOIDC(t *testing.T) {
	verifier, err := githubVerifier(context.Background(), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if verifier != nil {
		t.Fatal("verifier should be nil without allow-lists")
	}
}

func TestParsePositiveIntRejectsZero(t *testing.T) {
	if _, err := parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", "0"); err == nil {
		t.Fatal("expected zero to be rejected")
	}
}

func TestParsePositiveIntAcceptsPositiveValue(t *testing.T) {
	got, err := parsePositiveInt("VERSELF_DEPLOY_BAZEL_JOBS", "4")
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("value = %d, want 4", got)
	}
}
