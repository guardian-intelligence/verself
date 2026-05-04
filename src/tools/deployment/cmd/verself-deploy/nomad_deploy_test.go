package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/nomad/api"
)

type testNomadSpecParser struct {
	job *api.Job
}

func (p testNomadSpecParser) ParseJobHCL(context.Context, []byte, string) (*api.Job, error) {
	return p.job, nil
}

func testString(s string) *string {
	return &s
}

func TestAssembleNomadDeploymentBindsOnlyArtifactStanzas(t *testing.T) {
	repoRoot := t.TempDir()
	siteDir := filepath.Join(repoRoot, "src", "tools", "deployment", "nomad", "sites", "prod")
	if err := os.MkdirAll(siteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	siteConfig := `{
  "artifact_delivery": {
    "bucket": "nomad-artifacts",
    "key_prefix": "sha256",
    "getter_source_prefix": "s3::https://artifacts.internal.example/nomad-artifacts",
    "getter_options": {"region": "garage"},
    "publisher_credentials": {
      "environment_file": "/etc/garage/publisher.env",
      "access_key_id_env": "ACCESS",
      "secret_access_key_env": "SECRET"
    },
    "checksum_algorithm": "sha256",
    "public": false,
    "origin": {
      "scheme": "https",
      "hostname": "artifacts.internal.example",
      "port": 9443,
      "ca_bundle_path": "/etc/ca.pem"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(siteDir, "site.json"), []byte(siteConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactPath := filepath.Join(repoRoot, "bazel-out", "svc.tar")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("binary artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	specPath := filepath.Join(repoRoot, "src", "services", "svc", "nomad.hcl")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `job "svc" {}`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	descriptorPath := filepath.Join(repoRoot, "svc.nomad_component.json")
	descriptor := `{
  "schema_version": 1,
  "label": "//src/services/svc:nomad_component",
  "component": "svc",
  "depends_on": [],
  "job_id": "svc",
  "job_spec": "src/services/svc/nomad.hcl",
  "job_spec_path": "src/services/svc/nomad.hcl",
  "artifacts": [
    {
      "label": "//src/services/svc:svc_nomad_artifact",
      "output": "svc",
      "path": "bazel-out/svc.tar"
    }
  ]
}`
	if err := os.WriteFile(descriptorPath, []byte(descriptor), 0o644); err != nil {
		t.Fatal(err)
	}

	src := "verself-artifact://svc"
	dest := "local"
	deployment, err := assembleNomadDeployment(context.Background(), testNomadSpecParser{
		job: &api.Job{
			ID:   testString("svc"),
			Meta: map[string]string{},
			TaskGroups: []*api.TaskGroup{{
				Name: testString("svc"),
				Tasks: []*api.Task{{
					Name: "svc",
					Artifacts: []*api.TaskArtifact{{
						GetterSource: &src,
						RelativeDest: &dest,
					}},
					Env: map[string]string{
						"UNCHANGED": "verself-artifact://svc",
					},
				}},
			}},
		},
	}, repoRoot, "prod", []string{descriptorPath})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(deployment.Artifacts), 1; got != want {
		t.Fatalf("artifact count = %d, want %d", got, want)
	}
	if got, want := deployment.SubmitOrder, []string{"svc"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("submit order = %v, want %v", got, want)
	}

	var bound map[string]any
	if err := json.Unmarshal(deployment.Jobs[0].Spec, &bound); err != nil {
		t.Fatal(err)
	}
	task := bound["Job"].(map[string]any)["TaskGroups"].([]any)[0].(map[string]any)["Tasks"].([]any)[0].(map[string]any)
	artifact := task["Artifacts"].([]any)[0].(map[string]any)
	if got := artifact["GetterSource"].(string); got == "verself-artifact://svc" {
		t.Fatalf("artifact GetterSource was not bound")
	}
	if got := task["Env"].(map[string]any)["UNCHANGED"].(string); got != "verself-artifact://svc" {
		t.Fatalf("non-artifact string was changed to %q", got)
	}
	getterOptions := artifact["GetterOptions"].(map[string]any)
	if getterOptions["checksum"] == "" {
		t.Fatalf("artifact checksum was not set: %#v", getterOptions)
	}
}
