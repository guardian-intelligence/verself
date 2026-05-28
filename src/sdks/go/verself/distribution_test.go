package verself

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDistributionResolvesTargetsAndChecksUpdates(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	var authHeaders []string
	var checkBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/distribution/updates:check":
			if err := json.NewDecoder(r.Body).Decode(&checkBody); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"update_available":true,"target":` + distributionTargetJSON(digest) + `,"traceparent":"00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/distribution/packages/mksk/channels/canary/resolve":
			if r.URL.Query().Get("platform_os") != "linux" || r.URL.Query().Get("platform_arch") != "amd64" || r.URL.Query().Get("flavor") != "default" {
				t.Fatalf("unexpected resolve query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"target":` + distributionTargetJSON(digest) + `}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", DistributionURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	update, err := client.Distribution.CheckUpdate(context.Background(), DistributionCheckUpdateInput{
		PackageName:      DistributionProductMksk,
		ChannelName:      DistributionChannelCanary,
		PlatformOS:       "linux",
		PlatformArch:     "amd64",
		Flavor:           DistributionFlavorDefault,
		InstalledDigest:  "sha256:" + strings.Repeat("b", 64),
		InstalledVersion: "0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !update.UpdateAvailable || update.Target.ArtifactDigest != digest {
		t.Fatalf("unexpected update response: %#v", update)
	}
	if checkBody["package_name"] != "mksk" || checkBody["channel_name"] != "canary" || checkBody["installed_version"] != "0.1.0" {
		t.Fatalf("unexpected check body: %#v", checkBody)
	}
	if checkBody["flavor"] != "default" {
		t.Fatalf("unexpected check body flavor: %#v", checkBody)
	}
	target, err := client.Distribution.ResolveTarget(context.Background(), DistributionResolveTargetInput{
		PackageName:  DistributionProductMksk,
		ChannelName:  DistributionChannelCanary,
		PlatformOS:   "linux",
		PlatformArch: "amd64",
		Flavor:       DistributionFlavorDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.PublicOCIReference != "oci.verself.sh/verself/mksk@"+digest {
		t.Fatalf("unexpected target: %#v", target)
	}
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer tok_test" || authHeaders[1] != "Bearer tok_test" {
		t.Fatalf("authorization headers = %#v", authHeaders)
	}
}

func TestDistributionArtifactsAndChannelTargets(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	digestRef := "sha256-" + strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/distribution/artifacts/"+digestRef:
			_, _ = w.Write([]byte(`{"artifact":` + distributionArtifactJSON(digest) + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/distribution/packages/mksk/channels/stable/targets":
			if r.URL.Query().Get("page_size") != "50" {
				t.Fatalf("unexpected list query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"targets":[` + distributionTargetJSON(digest) + `],"next_page_token":"next"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", DistributionURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := client.Distribution.GetArtifact(context.Background(), DistributionGetArtifactInput{ArtifactDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ArtifactDigestRef != digestRef || artifact.Verification.Decision != "allowed" || len(artifact.Verification.Evidence) != 1 {
		t.Fatalf("unexpected artifact: %#v", artifact)
	}
	targets, err := client.Distribution.ListTargets(context.Background(), DistributionListTargetsOptions{
		PackageName: DistributionProductMksk,
		ChannelName: DistributionChannelStable,
		PageSize:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if targets.NextPageToken != "next" || len(targets.Targets) != 1 || targets.Targets[0].ArtifactDigest != digest {
		t.Fatalf("unexpected targets: %#v", targets)
	}
}

func TestDistributionErrorsNormalizeProblemDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"urn:verself:problem:distribution:artifact_quarantined","title":"Forbidden","status":403,"detail":"artifact is quarantined","code":"distribution.artifact_quarantined","traceparent":"00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01"}`))
	}))
	defer server.Close()

	client, err := New(Options{BearerToken: "tok_test", DistributionURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Distribution.ResolveTarget(context.Background(), DistributionResolveTargetInput{
		PackageName: DistributionProductMksk,
		ChannelName: DistributionChannelStable,
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want APIError", err, err)
	}
	if apiErr.Service != "Distribution API" || apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "distribution.artifact_quarantined" || !strings.Contains(apiErr.Error(), "artifact is quarantined") {
		t.Fatalf("unexpected api error: %#v", apiErr)
	}
}

func distributionTargetJSON(digest string) string {
	ref := strings.Replace(digest, ":", "-", 1)
	return `{"resourceName":"urn:verself:test:distribution/packages/mksk/channels/canary/targets/` + ref + `","package_name":"mksk","channel_name":"canary","platform_os":"linux","platform_arch":"amd64","flavor":"default","artifact_digest":"` + digest + `","artifact_digest_ref":"` + ref + `","package_version":"0.1.0","state":"published","public_oci_reference":"oci.verself.sh/verself/mksk@` + digest + `","download_url":"https://oci.verself.sh/v2/verself/mksk/manifests/` + digest + `","published_at":"2026-05-25T12:00:00Z"}`
}

func distributionArtifactJSON(digest string) string {
	ref := strings.Replace(digest, ":", "-", 1)
	return `{"artifact_digest":"` + digest + `","artifact_digest_ref":"` + ref + `","resourceName":"urn:verself:test:distribution/packages/mksk/artifacts/` + ref + `","package_name":"mksk","package_version":"0.1.0","platform_os":"linux","platform_arch":"amd64","flavor":"default","oci_repository":"verself/mksk","public_oci_reference":"oci.verself.sh/verself/mksk@` + digest + `","oci_media_type":"application/vnd.oci.image.manifest.v1+json","oci_size_bytes":4096,"state":"available","retention_class":"stable","verification":{"decision":"allowed","reason":"verified","builder_id":"spiffe://prod.verself.sh/svc/release-builder","signer_identity":"https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main","source_repository":"guardian-intelligence/verself","source_commit":"0123456789abcdef0123456789abcdef01234567","source_ref":"refs/heads/main","evidence":[{"evidence_kind":"slsa_provenance","predicate_type":"https://slsa.dev/provenance/v1","subject_digest":"` + digest + `","document_digest":"sha256:` + strings.Repeat("d", 64) + `","oci_referrer_digest":"sha256:` + strings.Repeat("e", 64) + `"}],"checked_at":"2026-05-25T12:00:00Z"},"replication":{"state":"available","origin_registry_url":"https://zot.service.consul","public_registry_url":"https://oci.verself.sh","checked_at":"2026-05-25T12:00:00Z"},"created_at":"2026-05-25T12:00:00Z","updated_at":"2026-05-25T12:00:00Z"}`
}
