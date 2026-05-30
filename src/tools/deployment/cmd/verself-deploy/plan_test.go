package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/verself/deployment-tools/internal/deploymodel"
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

func TestOrderNomadComponentsAddsArtifactOriginDependency(t *testing.T) {
	ordered, err := orderNomadComponents([]nomadComponentDescriptor{
		{
			JobID:     "web",
			Component: "web",
			Requires:  []string{"postgres:server"},
			Artifacts: []nomadDescriptorArtifact{{Label: "//src/web:bin", Output: "web", Path: "/tmp/web"}},
		},
		{
			JobID:     "postgres",
			Component: "postgres",
			Provides:  []string{"postgres:server"},
		},
		{
			JobID:     "garage",
			Component: "garage",
			Provides:  []string{"artifact-origin"},
		},
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	got := make([]string, 0, len(ordered))
	for _, component := range ordered {
		got = append(got, component.JobID)
	}
	want := []string{"garage", "postgres", "web"}
	if len(got) != len(want) {
		t.Fatalf("ordered len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordered[%d] = %q, want %q; full order: %v", i, got[i], want[i], got)
		}
	}
}

func TestOrderNomadComponentsAddsCredentialStoreDependency(t *testing.T) {
	ordered, err := orderNomadComponents([]nomadComponentDescriptor{
		{
			JobID:     "web",
			Component: "web",
			Artifacts: []nomadDescriptorArtifact{{Label: "//src/web:bin", Output: "web", Path: "/tmp/web"}},
		},
		{
			JobID:     "clickhouse-host-install",
			Component: "clickhouse_host_install",
			Provides:  []string{"clickhouse:host-tools"},
		},
		{
			JobID:     "credential-store",
			Component: "credential_store",
			Provides:  []string{"host:credstore"},
			Requires:  []string{"clickhouse:host-tools"},
		},
		{
			JobID:     "garage",
			Component: "garage",
			Provides:  []string{"artifact-origin"},
		},
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	got := make([]string, 0, len(ordered))
	for _, component := range ordered {
		got = append(got, component.JobID)
	}
	want := []string{"clickhouse-host-install", "credential-store", "garage", "web"}
	if len(got) != len(want) {
		t.Fatalf("ordered len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordered[%d] = %q, want %q; full order: %v", i, got[i], want[i], got)
		}
	}
}

func TestOrderNomadComponentsPlacesPublicOIDCEdgeBeforeIAM(t *testing.T) {
	ordered, err := orderNomadComponents([]nomadComponentDescriptor{
		{
			JobID:     "clickhouse-host-install",
			Component: "clickhouse_host_install",
			Provides:  []string{"clickhouse:host-tools"},
		},
		{
			JobID:     "credential-store",
			Component: "credential_store",
			Provides:  []string{"host:credstore"},
			Requires:  []string{"clickhouse:host-tools"},
		},
		{
			JobID:     "garage-artifact-origin",
			Component: "garage_artifact_origin",
			Provides:  []string{"artifact-origin"},
		},
		{
			JobID:     "postgresql",
			Component: "postgresql",
			Provides:  []string{"postgres:server"},
		},
		{
			JobID:     "zitadel",
			Component: "zitadel",
			Provides:  []string{"zitadel:http"},
			Requires:  []string{"postgres:server"},
		},
		{
			JobID:     "auth-control-plane",
			Component: "auth_control_plane",
			Provides:  []string{"auth:control-plane"},
			Requires:  []string{"zitadel:http"},
		},
		{
			JobID:     "haproxy-upstreams",
			Component: "haproxy_upstreams",
			Provides:  []string{"edge:public-oidc", "nomad:job:haproxy-upstreams"},
			Requires:  []string{"zitadel:http"},
			Artifacts: []nomadDescriptorArtifact{{Label: "//src/haproxy:runtime", Output: "haproxy-runtime", Path: "/tmp/haproxy"}},
		},
		{
			JobID:     "iam-service",
			Component: "iam_service",
			Requires:  []string{"auth:control-plane", "edge:public-oidc"},
		},
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	positions := map[string]int{}
	for i, component := range ordered {
		positions[component.JobID] = i
	}
	for _, jobID := range []string{"zitadel", "auth-control-plane", "haproxy-upstreams", "iam-service"} {
		if _, ok := positions[jobID]; !ok {
			t.Fatalf("%s missing from ordered components: %+v", jobID, positions)
		}
	}
	if positions["zitadel"] > positions["haproxy-upstreams"] {
		t.Fatalf("haproxy-upstreams ordered before zitadel: %+v", positions)
	}
	if positions["haproxy-upstreams"] > positions["iam-service"] {
		t.Fatalf("iam-service ordered before public OIDC edge: %+v", positions)
	}
	if positions["auth-control-plane"] > positions["iam-service"] {
		t.Fatalf("iam-service ordered before auth control plane: %+v", positions)
	}
}

func TestOrderNomadComponentsRequiresArtifactOriginForGarageArtifacts(t *testing.T) {
	_, err := orderNomadComponents([]nomadComponentDescriptor{
		{
			JobID:     "web",
			Component: "web",
			Artifacts: []nomadDescriptorArtifact{{Label: "//src/web:bin", Output: "web", Path: "/tmp/web"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no component provides artifact-origin") {
		t.Fatalf("err = %v, want missing artifact-origin", err)
	}
}

func TestOrderNomadComponentsRejectsArtifactOriginProviderWithGarageArtifacts(t *testing.T) {
	_, err := orderNomadComponents([]nomadComponentDescriptor{
		{
			JobID:     "garage",
			Component: "garage",
			Provides:  []string{"artifact-origin"},
			Artifacts: []nomadDescriptorArtifact{{Label: "//src/garage:bin", Output: "garage", Path: "/tmp/garage"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot provide artifact-origin with Garage-backed artifacts") {
		t.Fatalf("err = %v, want artifact-origin provider rejection", err)
	}
}

func TestPostDeployCanarySelectionAndExpansion(t *testing.T) {
	canaries := []deploymodel.PostDeployCanary{
		{
			Label:  "//src/example:cli_medium",
			Target: "//src/example:canary",
			Kind:   "cli",
			Size:   "medium",
			Args:   []string{"--site={site}", "--job={job_id}"},
		},
		{
			Label:  "//src/example:browser_large",
			Target: "//src/example:browser_canary",
			Kind:   "browser",
			Size:   "large",
		},
	}

	medium := selectPostDeployCanaries(canaries, canarySizeMedium)
	if len(medium) != 1 || medium[0].Label != "//src/example:cli_medium" {
		t.Fatalf("medium selection = %+v", medium)
	}
	all := selectPostDeployCanaries(canaries, canarySizeAll)
	if len(all) != 2 {
		t.Fatalf("all selection count = %d, want 2", len(all))
	}
	none := selectPostDeployCanaries(canaries, canarySizeNone)
	if len(none) != 0 {
		t.Fatalf("none selection count = %d, want 0", len(none))
	}

	expanded := expandCanaryValues(canaries[0].Args, map[string]string{
		"job_id": "iam-service",
		"site":   "prod",
	})
	want := []string{"--site=prod", "--job=iam-service"}
	if len(expanded) != len(want) {
		t.Fatalf("expanded count = %d, want %d: %v", len(expanded), len(want), expanded)
	}
	for i := range want {
		if expanded[i] != want[i] {
			t.Fatalf("expanded[%d] = %q, want %q", i, expanded[i], want[i])
		}
	}
}

func TestValidatePostDeployCanary(t *testing.T) {
	valid := deploymodel.PostDeployCanary{
		Label:   "//src/example:canary",
		Target:  "//src/example:runner",
		Kind:    "browser",
		Size:    "large",
		Timeout: "10m",
	}
	if err := validatePostDeployCanary(valid); err != nil {
		t.Fatalf("valid canary rejected: %v", err)
	}
	invalid := valid
	invalid.Size = "small"
	if err := validatePostDeployCanary(invalid); err == nil {
		t.Fatalf("invalid size was accepted")
	}
	invalid = valid
	invalid.Timeout = "eventually"
	if err := validatePostDeployCanary(invalid); err == nil {
		t.Fatalf("invalid timeout was accepted")
	}
}
