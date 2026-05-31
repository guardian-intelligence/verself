package releaseattest

import "testing"

func TestReleaseInputDigestUsesCanonicalTranscript(t *testing.T) {
	input := ReleaseInput{
		DistributionChallenge:      "challenge-20260530",
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
	got, err := input.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	const want = "sha256:7ec906f61e0ccfd626bc6f4d8259dd9f79e836fa3de1baa36eaa1f00b87ae7b6"
	if got != want {
		t.Fatalf("Digest() = %s, want %s", got, want)
	}
}

func TestDigestRejectsMissingFields(t *testing.T) {
	_, err := (ReleaseInput{}).Digest()
	if err == nil {
		t.Fatal("Digest() error = nil")
	}
}

func TestDigestBytes(t *testing.T) {
	got, err := DigestBytes(digest("a"))
	if err != nil {
		t.Fatalf("DigestBytes() error = %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("DigestBytes() length = %d, want 32", len(got))
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
