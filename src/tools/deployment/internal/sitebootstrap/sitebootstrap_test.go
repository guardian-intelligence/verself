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
	if err == nil || !strings.Contains(err.Error(), "read inventory") {
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

func TestValidateLocalOpenBaoSiteRootTokenFileRequiresPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site-root-token")
	if err := os.WriteFile(path, []byte("token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateLocalOpenBaoSiteRootTokenFile(path)
	if err == nil || !strings.Contains(err.Error(), "readable only by the operator") {
		t.Fatalf("error = %v, want private mode rejection", err)
	}
}

func TestOpenBaoSiteRootTokenInstallCommandUsesRunPath(t *testing.T) {
	command := openBaoSiteRootTokenInstallCommand("/tmp/root-token", openBaoSiteRootTokenPath)
	for _, want := range []string{"/run/verself/bootstrap/openbao-site-root.token", "install -d", "-m 0700", "-m 0600", "trap 'rm -f \"$tmp\"' EXIT"} {
		if !strings.Contains(command, want) {
			t.Fatalf("install command missing %q:\n%s", want, command)
		}
	}
}

func TestSCPCommandUsesUppercasePortFlag(t *testing.T) {
	cmd := scpCommand(context.Background(), inventoryTarget{Host: "2001:db8::1", User: "ubuntu", Port: 2222}, "/tmp/local", "/tmp/remote")
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{"-P", "2222", "ubuntu@[2001:db8::1]:/tmp/remote"} {
		if !strings.Contains(args, want) {
			t.Fatalf("scp args missing %q:\n%s", want, args)
		}
	}
}

func TestRemoteArtifactFileUsesDigestAndSafeSegments(t *testing.T) {
	got := remoteArtifactFile(bootstrapArtifactRoot, "..", ".", deployengine.ArtifactPublishCandidate{
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
	got := remoteArtifactGetterSource("gamma", "sha", deployengine.ArtifactPublishCandidate{
		Output: "openbao-runtime",
		SHA256: strings.Repeat("b", 64),
	})

	if strings.HasPrefix(got, "file:") {
		t.Fatalf("getter source uses unsupported file scheme: %s", got)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:7380/gamma/sha/") {
		t.Fatalf("getter source = %q", got)
	}
}

func TestExternalRuntimeSecretImportErrorListsAllMissingSecrets(t *testing.T) {
	err := externalRuntimeSecretImportError([]string{"z.secret", "a.secret"})

	want := "external OpenBao runtime secrets are not imported: a.secret, z.secret"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
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
