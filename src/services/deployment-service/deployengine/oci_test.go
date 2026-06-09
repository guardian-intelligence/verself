package deployengine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestBindNomadOCIImagesBuildsDigestReferences(t *testing.T) {
	root := t.TempDir()
	digestPath := filepath.Join(root, "bazel-out", "analytics_service_image.json.sha256")
	manifestBody := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json"}`)
	sum := sha256.Sum256(manifestBody)
	digestHex := hex.EncodeToString(sum[:])
	digest := "sha256:" + digestHex
	if err := os.MkdirAll(filepath.Dir(digestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(digestPath, []byte(digest+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(strings.TrimSuffix(strings.TrimSuffix(digestPath, ".sha256"), ".json"), "blobs", "sha256", digestHex)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestBody, 0o644); err != nil {
		t.Fatal(err)
	}

	bindings, err := bindNomadOCIImages(root, testOCIRegistryPolicy(), []nomadComponentDescriptor{
		{
			Label: "analytics",
			OCIImages: []nomadDescriptorOCIImage{
				{
					ImageLabel: "//src/services/analytics-service/cmd/analytics-service:analytics_service_image.digest",
					Output:     "analytics-service",
					DigestPath: "bazel-out/analytics_service_image.json.sha256",
					PushLabel:  "//src/services/analytics-service/cmd/analytics-service:analytics_service_image_push",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := bindings["analytics-service"]
	if binding.Digest != digest {
		t.Fatalf("digest = %q, want %q", binding.Digest, digest)
	}
	if binding.MediaType != "application/vnd.oci.image.index.v1+json" || binding.SizeBytes == 0 {
		t.Fatalf("manifest metadata = %q/%d", binding.MediaType, binding.SizeBytes)
	}
	if binding.Repository != "verself/analytics-service" {
		t.Fatalf("repository = %q", binding.Repository)
	}
	if binding.PushRepository != "127.0.0.1:5080/verself/analytics-service" {
		t.Fatalf("push repository = %q", binding.PushRepository)
	}
	wantPull := "127.0.0.1:5080/verself/analytics-service@" + digest
	if binding.PullReference != wantPull {
		t.Fatalf("pull reference = %q, want %q", binding.PullReference, wantPull)
	}
}

func TestAdmitOCIImagesPublishesEvidenceBeforeDistributionAdmission(t *testing.T) {
	digest := "sha256:" + strings.Repeat("e", 64)
	evidence := []DeploymentImageEvidence{{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     predicateSLSAProvenance,
		SubjectDigest:     digest,
		DocumentDigest:    "sha256:" + strings.Repeat("f", 64),
		OCIReferrerDigest: "sha256:" + strings.Repeat("1", 64),
	}}
	publisher := &recordingEvidencePublisher{evidence: evidence}
	admitter := &recordingDistributionAdmitter{}
	inputs := &deployInputs{
		SHA:          strings.Repeat("d", 40),
		DeployRunKey: "deploy_123",
		OCIImages: map[string]ociImageBinding{
			"analytics-service": {
				Output:            "analytics-service",
				ImageLabel:        "//src/services/analytics-service/cmd/analytics-service:analytics_service_image.digest",
				PushLabel:         "//src/services/analytics-service/cmd/analytics-service:analytics_service_image_push",
				Digest:            digest,
				MediaType:         "application/vnd.oci.image.index.v1+json",
				SizeBytes:         512,
				Repository:        "verself/analytics-service",
				PushRepository:    "127.0.0.1:5080/verself/analytics-service",
				PullReference:     "127.0.0.1:5080/verself/analytics-service@" + digest,
				OriginRegistryURL: "http://127.0.0.1:5080",
				PublicRegistryURL: "https://oci.verself.sh",
				DockerConfigDir:   "/var/lib/verself/deployment-service/.docker",
				Insecure:          true,
			},
		},
	}
	exec := testExecution()
	exec.BuilderID = "spiffe://gamma.verself.sh/svc/deployment-service"
	exec.SourceRepository = "guardian-intelligence/verself"
	exec.SourceRef = "refs/heads/main"
	exec.OCIEvidencePublisher = publisher
	exec.DistributionAdmitter = admitter
	if err := admitOCIImages(context.Background(), exec, inputs); err != nil {
		t.Fatal(err)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("evidence requests = %d, want 1", len(publisher.requests))
	}
	if len(admitter.requests) != 1 {
		t.Fatalf("admission requests = %d, want 1", len(admitter.requests))
	}
	req := admitter.requests[0]
	if req.PolicyRef != deploymentOCIPolicyRef || req.BuilderID != exec.BuilderID || req.SourceRepository != exec.SourceRepository {
		t.Fatalf("unexpected admission request: %+v", req)
	}
	if len(req.Evidence) != 1 || req.Evidence[0].DocumentDigest != evidence[0].DocumentDigest {
		t.Fatalf("admission evidence = %+v", req.Evidence)
	}
}

func TestAdmitOCIImagesAllowsExplicitBootstrapWithoutDistributionAdmission(t *testing.T) {
	digest := "sha256:" + strings.Repeat("e", 64)
	publisher := &recordingEvidencePublisher{evidence: []DeploymentImageEvidence{{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     predicateSLSAProvenance,
		SubjectDigest:     digest,
		DocumentDigest:    "sha256:" + strings.Repeat("f", 64),
		OCIReferrerDigest: "sha256:" + strings.Repeat("1", 64),
	}}}
	inputs := &deployInputs{
		SHA:          strings.Repeat("d", 40),
		DeployRunKey: "bootstrap_123",
		OCIImages: map[string]ociImageBinding{
			"analytics-service": {
				Output:         "analytics-service",
				ImageLabel:     "//src/services/analytics-service/cmd/analytics-service:analytics_service_image.digest",
				PushLabel:      "//src/services/analytics-service/cmd/analytics-service:analytics_service_image_push",
				Digest:         digest,
				MediaType:      "application/vnd.oci.image.index.v1+json",
				SizeBytes:      512,
				Repository:     "verself/analytics-service",
				PushRepository: "127.0.0.1:5080/verself/analytics-service",
				PullReference:  "127.0.0.1:5080/verself/analytics-service@" + digest,
			},
		},
	}
	var stdout bytes.Buffer
	exec := testExecution()
	exec.BuilderID = "verself://site-bootstrap/gamma"
	exec.SourceRepository = "guardian-intelligence/verself"
	exec.SourceRef = "refs/heads/main"
	exec.OCIEvidencePublisher = publisher
	exec.AllowBootstrapOCIWithoutDistributionAdmission = true
	exec.Stdout = &stdout
	if err := admitOCIImages(context.Background(), exec, inputs); err != nil {
		t.Fatal(err)
	}
	if len(publisher.requests) != 1 {
		t.Fatalf("evidence requests = %d, want 1", len(publisher.requests))
	}
	if !strings.Contains(stdout.String(), "bootstrap OCI distribution admission skipped") {
		t.Fatalf("stdout did not report bootstrap admission skip: %q", stdout.String())
	}
}

func TestBindOCIImagesInSpecBindsDigestReference(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	job := &api.Job{
		TaskGroups: []*api.TaskGroup{
			{
				Tasks: []*api.Task{
					{
						Name:   "analytics-service",
						Driver: "podman",
						Config: map[string]interface{}{
							"image": "verself-oci://analytics-service",
						},
					},
				},
			},
		},
	}
	seen, err := bindOCIImagesInSpec(job, map[string]ociImageBinding{
		"analytics-service": {
			Output:        "analytics-service",
			Digest:        digest,
			PullReference: "127.0.0.1:5080/verself/analytics-service@" + digest,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !seen["analytics-service"] {
		t.Fatal("OCI image was not marked as seen")
	}
	task := job.TaskGroups[0].Tasks[0]
	if got := task.Config["image"]; got != "127.0.0.1:5080/verself/analytics-service@"+digest {
		t.Fatalf("image = %q", got)
	}
	if len(task.Artifacts) != 0 {
		t.Fatalf("artifacts = %d, want none", len(task.Artifacts))
	}
}

func TestPublishOCIImagesUsesDeclaredPusher(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	pusher := &recordingOCIPusher{}
	inputs := &deployInputs{
		SHA:          strings.Repeat("d", 40),
		DeployRunKey: "deploy_123",
		OCIImages: map[string]ociImageBinding{
			"analytics-service": {
				Output:          "analytics-service",
				Digest:          digest,
				Repository:      "verself/analytics-service",
				PushRepository:  "127.0.0.1:5080/verself/analytics-service",
				PullReference:   "127.0.0.1:5080/verself/analytics-service@" + digest,
				PushLabel:       "//src/services/analytics-service/cmd/analytics-service:analytics_service_image_push",
				DockerConfigDir: "/var/lib/verself/deployment-service/.docker",
				Insecure:        true,
			},
		},
	}
	exec := testExecution()
	exec.OCIPusher = pusher
	exec.BazelBuildFlags = []string{"--jobs=4"}
	err := publishOCIImages(context.Background(), exec, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(pusher.requests) != 1 {
		t.Fatalf("push requests = %d, want 1", len(pusher.requests))
	}
	req := pusher.requests[0]
	if req.PushRepository != "127.0.0.1:5080/verself/analytics-service" || req.Digest != digest || !req.Insecure {
		t.Fatalf("unexpected push request: %+v", req)
	}
	if len(req.BazelBuildFlags) != 1 || req.BazelBuildFlags[0] != "--jobs=4" {
		t.Fatalf("bazel flags = %v", req.BazelBuildFlags)
	}
}

type recordingOCIPusher struct {
	requests []OCIPushRequest
}

func (p *recordingOCIPusher) PushDeploymentImage(_ context.Context, req OCIPushRequest) error {
	p.requests = append(p.requests, req)
	return nil
}

type recordingEvidencePublisher struct {
	requests []DeploymentEvidencePublishRequest
	evidence []DeploymentImageEvidence
}

func (p *recordingEvidencePublisher) PublishDeploymentEvidence(_ context.Context, req DeploymentEvidencePublishRequest) ([]DeploymentImageEvidence, error) {
	p.requests = append(p.requests, req)
	return p.evidence, nil
}

type recordingDistributionAdmitter struct {
	requests []DeploymentImageAdmissionRequest
}

func (a *recordingDistributionAdmitter) AdmitDeploymentImage(_ context.Context, req DeploymentImageAdmissionRequest) (DeploymentImageAdmissionResult, error) {
	a.requests = append(a.requests, req)
	return DeploymentImageAdmissionResult{PublicOCIReference: "oci.verself.sh/" + req.Repository + "@" + req.Digest}, nil
}

func testOCIRegistryPolicy() ociRegistryPolicy {
	return ociRegistryPolicy{
		OriginRegistryURL: "http://127.0.0.1:5080",
		PublicRegistryURL: "https://oci.verself.sh",
		PushRegistry:      "127.0.0.1:5080",
		PullRegistry:      "127.0.0.1:5080",
		RepositoryPrefix:  "verself",
		DockerConfigDir:   "/var/lib/verself/deployment-service/.docker",
		Insecure:          true,
	}
}
