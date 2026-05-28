package api

import "testing"

func TestAuthProblemTypeUsesDurableURN(t *testing.T) {
	got := problemType("auth.session_revoked")
	want := "urn:verself:problem:auth:session_revoked"
	if got != want {
		t.Fatalf("problemType() = %q, want %q", got, want)
	}
}
