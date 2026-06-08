package sitebootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verself/deployment-service/deployengine"
)

func TestBootstrapDeployReadsInventoryBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "missing-inventory.ini"),
	})
	if err == nil || !strings.Contains(err.Error(), "open inventory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyArtifactCandidateChecksBodyDigest(t *testing.T) {
	body := []byte("artifact-bytes")
	err := verifyArtifactCandidate(deployengine.ArtifactPublishCandidate{
		Output: "runtime",
		SHA256: testSHA256(body),
		Body:   body,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = verifyArtifactCandidate(deployengine.ArtifactPublishCandidate{
		Output: "runtime",
		SHA256: strings.Repeat("0", 64),
		Body:   body,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("error = %v, want digest mismatch", err)
	}
}

func TestLocalArtifactUploadPathWritesBody(t *testing.T) {
	body := []byte("bundle")
	path, cleanup, err := localArtifactUploadPath(deployengine.ArtifactPublishCandidate{Output: "bundle", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := readFile(t, path); got != string(body) {
		t.Fatalf("temporary artifact body = %q", got)
	}
}

func TestLoadBootstrapInventoryTargetUsesRecoveryTransport(t *testing.T) {
	root := t.TempDir()
	inventory := filepath.Join(root, "inventory.ini")
	if err := os.WriteFile(inventory, []byte(`[infra]
node-1 ansible_host=access.example.test verself_recovery_ssh_host=10.66.66.1 verself_recovery_ssh_port=2222

[all:vars]
ansible_user=ubuntu@example
`), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := loadBootstrapInventoryTarget(inventory, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "10.66.66.1" || target.User != "ubuntu@example" || target.Port != 2222 {
		t.Fatalf("target = %s@%s:%d", target.User, target.Host, target.Port)
	}
}

func TestRemoteArtifactFileUsesDigestAndSafeSegments(t *testing.T) {
	got := remoteArtifactFile("/var/lib/verself/bootstrap/artifacts", "..", ".", deployengine.ArtifactPublishCandidate{
		Output: "../service/runtime",
		SHA256: strings.Repeat("a", 64),
	})

	if strings.Contains(got, "/../") || strings.Contains(got, "/./") {
		t.Fatalf("remote artifact path is not sanitized: %s", got)
	}
	if !strings.Contains(got, "/_/_/") || !strings.Contains(got, strings.Repeat("a", 64)+"-.._service_runtime.tar") {
		t.Fatalf("remote artifact path missing digest-safe output name: %s", got)
	}
}

func TestRemoteArtifactGetterSourceUsesLoopbackHTTP(t *testing.T) {
	remoteFile := remoteArtifactFile("/var/lib/verself/bootstrap/artifacts", "gamma", "abc", deployengine.ArtifactPublishCandidate{
		Output: "openbao-runtime",
		SHA256: strings.Repeat("b", 64),
	})
	got, err := remoteArtifactGetterSource("http://127.0.0.1:18733", "/var/lib/verself/bootstrap/artifacts", remoteFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:18733/gamma/abc/") || !strings.HasSuffix(got, "-openbao-runtime.tar") {
		t.Fatalf("getter source = %q", got)
	}
}

func TestRemoteArtifactGetterSourceRejectsOutsideRoot(t *testing.T) {
	_, err := remoteArtifactGetterSource("http://127.0.0.1:18733", "/var/lib/verself/bootstrap/artifacts", "/tmp/openbao-runtime.tar")
	if err == nil || !strings.Contains(err.Error(), "outside bootstrap root") {
		t.Fatalf("error = %v, want outside root", err)
	}
}

func TestParseRemotePasswdEntry(t *testing.T) {
	identity, err := parseRemotePasswdEntry("deployment_service", "deployment_service:x:990:984::/home/deployment_service:/usr/sbin/nologin\n")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 990 || identity.GID != 984 {
		t.Fatalf("identity = uid:%d gid:%d, want uid:990 gid:984", identity.UID, identity.GID)
	}
}

func TestParseRemotePasswdEntryRejectsMalformedOutput(t *testing.T) {
	err := func() error {
		_, err := parseRemotePasswdEntry("deployment_service", "deployment_service:x:not-a-uid:984\n")
		return err
	}()
	if err == nil || !strings.Contains(err.Error(), "parse remote uid") {
		t.Fatalf("error = %v, want parse remote uid error", err)
	}
}

func TestWriteInventory(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "inventory.ini")
	if err := WriteInventory(InventoryOptions{
		Site:       "gamma",
		Host:       "203.0.113.10",
		OutputPath: out,
		ForceWrite: true,
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"[workers]", "vs-gamma-w0 ansible_host=203.0.113.10", "ansible_user=ubuntu"} {
		if !strings.Contains(text, want) {
			t.Fatalf("inventory missing %q:\n%s", want, text)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func testSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
