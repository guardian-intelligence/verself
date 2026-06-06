package toolcatalog

import (
	"strings"
	"testing"
)

func TestResolveDigestAddressedTool(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	resolved, err := ResolveFrom(Catalog{
		Tools: map[string]Tool{
			"bazel": {
				Platforms: map[string]PlatformTool{
					"linux/amd64": {
						Ref:        "oci.verself.sh/tools/bazel@" + digest,
						Executable: "bazel",
						Admission:  "admitted",
					},
				},
			},
		},
	}, "bazel", "linux/amd64")
	if err != nil {
		t.Fatalf("ResolveFrom: %v", err)
	}
	if resolved.Digest != digest {
		t.Fatalf("Digest = %q, want %q", resolved.Digest, digest)
	}
}

func TestResolveRejectsMutableRef(t *testing.T) {
	_, err := ResolveFrom(Catalog{
		Tools: map[string]Tool{
			"bazel": {
				Platforms: map[string]PlatformTool{
					"linux/amd64": {
						Ref:        "oci.verself.sh/tools/bazel:9.1.0",
						Executable: "bazel",
						Admission:  "admitted",
					},
				},
			},
		},
	}, "bazel", "linux/amd64")
	if err == nil {
		t.Fatal("ResolveFrom succeeded, want mutable ref rejection")
	}
	if !strings.Contains(err.Error(), "must be digest-addressed") {
		t.Fatalf("error = %q, want digest-addressed", err)
	}
}
