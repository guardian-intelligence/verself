package api

import "testing"

func TestAuthProblemTypePointsAtPublicAnchor(t *testing.T) {
	got := problemType("auth.session_revoked")
	want := "https://verself.sh/docs/reference/iam/errors#auth-session-revoked"
	if got != want {
		t.Fatalf("problemType() = %q, want %q", got, want)
	}
}
