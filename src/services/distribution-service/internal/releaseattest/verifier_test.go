package releaseattest

import (
	"context"
	"strings"
	"testing"
	"time"

	releaseinput "github.com/verself/releaseattest"
)

func TestVerifierRejectsDigestMismatchBeforeTPM(t *testing.T) {
	input := testInput()
	verifier := Verifier{}
	_, err := verifier.Verify(context.Background(), Request{
		Input:              input,
		ReleaseInputDigest: digest("0"),
		BuilderID:          "spiffe://prod.verself.sh/svc/release-builder",
		PolicyID:           "release-builder-v1",
		CheckedAt:          time.Now(),
	})
	if err == nil {
		t.Fatal("Verify() error = nil")
	}
}

func TestVerifyPCRPolicyRequiresExpectedValues(t *testing.T) {
	err := verifyPCRPolicy(PCRPolicy{ID: "empty"}, []PCR{{Index: 0, DigestAlg: "sha256", Digest: digest("a")}})
	if err == nil {
		t.Fatal("verifyPCRPolicy() error = nil")
	}
}

func TestVerifyPCRPolicyMatchesExpectedValues(t *testing.T) {
	err := verifyPCRPolicy(PCRPolicy{
		ID: "release-builder-v1",
		PCRs: []PCR{
			{Index: 0, DigestAlg: "sha256", Digest: digest("a")},
			{Index: 2, DigestAlg: "sha256", Digest: digest("b")},
		},
	}, []PCR{
		{Index: 0, DigestAlg: "sha256", Digest: digest("a")},
		{Index: 2, DigestAlg: "sha256", Digest: digest("b")},
		{Index: 7, DigestAlg: "sha256", Digest: digest("c")},
	})
	if err != nil {
		t.Fatalf("verifyPCRPolicy() error = %v", err)
	}
}

func TestVerifierRejectsTPMReleaseKeyMismatch(t *testing.T) {
	input := testInput()
	releaseInputDigest, err := input.Digest()
	if err != nil {
		t.Fatal(err)
	}
	verifier := Verifier{
		TrustedAKs: map[string]TrustedAK{
			digestBytesFor(nil): {BuilderID: "spiffe://prod.verself.sh/svc/release-builder"},
		},
		PCRPolicies: map[string]PCRPolicy{
			"release-builder-v1": {
				ID:        "release-builder-v1",
				BuilderID: "spiffe://prod.verself.sh/svc/release-builder",
				PCRs:      []PCR{{Index: 0, DigestAlg: "sha256", Digest: digest("a")}},
			},
		},
	}
	_, err = verifier.Verify(context.Background(), Request{
		Input:              input,
		ReleaseInputDigest: releaseInputDigest,
		BuilderID:          "spiffe://prod.verself.sh/svc/release-builder",
		PolicyID:           "release-builder-v1",
		TPM:                TPMEvidence{ReleasePublicName: "000b" + sixtyFour("0"), ReleasePublicBlobDigest: input.TPMReleasePublicBlobDigest},
		CheckedAt:          time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "TPM release public name does not match") {
		t.Fatalf("Verify() error = %v, want release key mismatch", err)
	}
}

func TestStaticOpenBaoAuthorizerBindsReleaseDigestAndKey(t *testing.T) {
	result := Result{
		ReleaseInputDigest:   digest("a"),
		TPMReleasePublicName: "000b" + sixtyFour("b"),
	}
	authorization, err := (StaticOpenBaoAuthorizer{TransitMount: "transit", KeyName: "release-root"}).AuthorizeRelease(context.Background(), result)
	if err != nil {
		t.Fatalf("AuthorizeRelease() error = %v", err)
	}
	if authorization.ReleaseInputDigest != result.ReleaseInputDigest {
		t.Fatalf("authorization digest = %s, want %s", authorization.ReleaseInputDigest, result.ReleaseInputDigest)
	}
	if authorization.TPMReleaseKeyName != result.TPMReleasePublicName {
		t.Fatalf("authorization key name = %s, want %s", authorization.TPMReleaseKeyName, result.TPMReleasePublicName)
	}
}

func testInput() releaseinput.ReleaseInput {
	return releaseinput.ReleaseInput{
		DistributionChallenge:      "challenge",
		Package:                    "mksk",
		Version:                    "0.2.0-nightly.20260530.1",
		SourceCommit:               "0123456789abcdef0123456789abcdef01234567",
		Platform:                   "linux/amd64",
		Flavor:                     "default",
		OCIManifestDigest:          digest("a"),
		ArtifactDigest:             digest("b"),
		ProvenanceDigest:           digest("c"),
		SBOMDigest:                 digest("d"),
		TPMReleasePublicName:       "000b" + sixtyFour("e"),
		TPMReleasePublicBlobDigest: digest("f"),
	}
}

func digest(value string) string {
	return "sha256:" + sixtyFour(value)
}

func sixtyFour(value string) string {
	out := ""
	for len(out) < 64 {
		out += value
	}
	return out[:64]
}
