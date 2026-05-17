package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestComponentInputDigestUsesContentAndStableOrdering(t *testing.T) {
	repoRoot := t.TempDir()
	firstPath := filepath.Join(repoRoot, "src", "component", "a.txt")
	secondPath := filepath.Join(repoRoot, "src", "component", "b.txt")
	if err := os.MkdirAll(filepath.Dir(firstPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(firstPath, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	component := nomadComponentDescriptor{
		DigestInputs: []nomadDescriptorInput{
			{Label: "//src/component:role_sources", Path: "src/component/b.txt", ShortPath: "src/component/b.txt"},
			{Label: "//src/component:role_sources", Path: "src/component/a.txt", ShortPath: "src/component/a.txt"},
		},
	}
	digest, err := componentInputDigest(repoRoot, component)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	reordered := nomadComponentDescriptor{
		DigestInputs: []nomadDescriptorInput{
			{Label: "//src/component:role_sources", Path: "src/component/a.txt", ShortPath: "src/component/a.txt"},
			{Label: "//src/component:role_sources", Path: "src/component/b.txt", ShortPath: "src/component/b.txt"},
		},
	}
	reorderedDigest, err := componentInputDigest(repoRoot, reordered)
	if err != nil {
		t.Fatalf("reordered digest: %v", err)
	}
	if digest != reorderedDigest {
		t.Fatalf("digest changed with descriptor ordering: %s != %s", digest, reorderedDigest)
	}

	if err := os.WriteFile(secondPath, []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("rewrite second: %v", err)
	}
	changedDigest, err := componentInputDigest(repoRoot, component)
	if err != nil {
		t.Fatalf("changed digest: %v", err)
	}
	if digest == changedDigest {
		t.Fatalf("digest did not change after digest input content changed: %s", digest)
	}
}

func TestStampNomadSpecMetaIncludesInputDigest(t *testing.T) {
	jobID := "haproxy-upstreams"
	job := &api.Job{
		ID:          &jobID,
		Name:        &jobID,
		Datacenters: []string{"dc1"},
		Meta: map[string]string{
			"deploy_run_key": "old-run",
			"deploy_sha":     "old-sha",
			"input_sha256":   "old-input",
			"spec_sha256":    "old-spec",
		},
	}

	firstSpec, err := stampNomadSpecMeta(job, "artifact", "input-one", "run", "sha")
	if err != nil {
		t.Fatalf("stamp first: %v", err)
	}
	if job.Meta["input_sha256"] != "input-one" {
		t.Fatalf("input_sha256 = %q, want input-one", job.Meta["input_sha256"])
	}
	if job.Meta["artifact_sha256"] != "artifact" {
		t.Fatalf("artifact_sha256 = %q, want artifact", job.Meta["artifact_sha256"])
	}
	if job.Meta["deploy_run_key"] != "run" || job.Meta["deploy_sha"] != "sha" {
		t.Fatalf("deploy metadata not restored after digest: %+v", job.Meta)
	}

	secondSpec, err := stampNomadSpecMeta(job, "artifact", "input-two", "run", "sha")
	if err != nil {
		t.Fatalf("stamp second: %v", err)
	}
	if firstSpec == secondSpec {
		t.Fatalf("spec digest did not change after input digest changed: %s", firstSpec)
	}
}
