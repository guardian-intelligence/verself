package ocipublish

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/memory"
)

func TestPublishCreatesSubjectAndReferrers(t *testing.T) {
	root := t.TempDir()
	artifact := writeTestFile(t, root, "make-skill.tar", "artifact")
	sbom := writeTestFile(t, root, "sbom.spdx.json", `{"spdxVersion":"SPDX-2.3"}`)
	provenance := writeTestFile(t, root, "provenance.intoto.json", `{"predicateType":"https://slsa.dev/provenance/v1"}`)
	target := memory.New()

	result, err := Publish(context.Background(), target, Request{
		Reference: "mksk-v0.2.0-nightly.20260528.1-linux-amd64-default",
		Subject: Manifest{
			ArtifactType: ArtifactTypeRelease,
			Files: []File{{
				Path:      artifact,
				MediaType: MediaTypeReleaseTar,
				Annotations: map[string]string{
					v1.AnnotationTitle: "make-skill.tar",
				},
			}},
			Annotations: map[string]string{
				v1.AnnotationTitle: "mksk 0.2.0-nightly.20260528.1",
			},
		},
		Referrers: []Manifest{
			{
				ArtifactType: MediaTypeSPDX,
				Files: []File{{
					Path:      sbom,
					MediaType: MediaTypeSPDX,
					Annotations: map[string]string{
						AnnotationEvidenceKind: "sbom",
					},
				}},
			},
			{
				ArtifactType: MediaTypeInToto,
				Files: []File{{
					Path:      provenance,
					MediaType: MediaTypeInToto,
					Annotations: map[string]string{
						AnnotationEvidenceKind: "provenance",
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if result.Subject.Descriptor.Digest.String() == "" {
		t.Fatal("subject digest is empty")
	}
	if len(result.Referrers) != 2 {
		t.Fatalf("referrers = %d, want 2", len(result.Referrers))
	}
	resolved, err := target.Resolve(context.Background(), result.Reference)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Digest != result.Subject.Descriptor.Digest {
		t.Fatalf("resolved digest = %s, want %s", resolved.Digest, result.Subject.Descriptor.Digest)
	}
	predecessors, err := target.Predecessors(context.Background(), result.Subject.Descriptor)
	if err != nil {
		t.Fatalf("Predecessors() error = %v", err)
	}
	if len(predecessors) != 2 {
		t.Fatalf("predecessors = %d, want 2", len(predecessors))
	}
	if err := Verify(context.Background(), target, result); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestPublishRefusesToMoveExistingReference(t *testing.T) {
	root := t.TempDir()
	target := memory.New()
	req := Request{
		Reference: "mksk-v0.2.0-linux-amd64-default",
		Subject: Manifest{
			ArtifactType: ArtifactTypeRelease,
			Files: []File{{
				Path:      writeTestFile(t, root, "make-skill.tar", "artifact one"),
				MediaType: MediaTypeReleaseTar,
			}},
		},
	}
	if _, err := Publish(context.Background(), target, req); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	req.Subject.Files[0].Path = writeTestFile(t, root, "make-skill-v2.tar", "artifact two")
	_, err := Publish(context.Background(), target, req)
	if err == nil {
		t.Fatal("Publish() error = nil")
	}
	if !strings.Contains(err.Error(), "refusing to retag") {
		t.Fatalf("Publish() error = %v, want retag refusal", err)
	}
}

func TestParseRegistryEndpoint(t *testing.T) {
	endpoint, plainHTTP, err := parseRegistryEndpoint("http://127.0.0.1:5080")
	if err != nil {
		t.Fatalf("parseRegistryEndpoint() error = %v", err)
	}
	if endpoint != "127.0.0.1:5080" || !plainHTTP {
		t.Fatalf("parseRegistryEndpoint() = %q %t", endpoint, plainHTTP)
	}
	endpoint, plainHTTP, err = parseRegistryEndpoint("oci.verself.sh")
	if err != nil {
		t.Fatalf("parseRegistryEndpoint() error = %v", err)
	}
	if endpoint != "oci.verself.sh" || plainHTTP {
		t.Fatalf("parseRegistryEndpoint() = %q %t", endpoint, plainHTTP)
	}
	if _, _, err := parseRegistryEndpoint("https://oci.verself.sh/verself/mksk"); err == nil {
		t.Fatal("parseRegistryEndpoint() error = nil for repository path")
	}
}

func writeTestFile(t *testing.T, root string, name string, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
