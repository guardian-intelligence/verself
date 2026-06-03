package sitebootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapDeployChecksArtifactPublishingBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:          "gamma",
		SHA:           "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:      root,
		InventoryPath: filepath.Join(root, "missing-inventory.ini"),
	})
	if err == nil || !strings.Contains(err.Error(), "--cloudflare-control-plane-binary") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "inventory") || strings.Contains(err.Error(), "OpenBao site root key") {
		t.Fatalf("bootstrap deploy reached remote-facing checks before local artifact publishing validation: %v", err)
	}
}

func TestBootstrapDeployChecksR2PublisherBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	cloudflareBinary := filepath.Join(root, "cloudflare-control-plane")
	if err := os.WriteFile(cloudflareBinary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:             "gamma",
		SHA:              "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:         root,
		InventoryPath:    filepath.Join(root, "missing-inventory.ini"),
		CloudflareBinary: cloudflareBinary,
	})
	if err == nil || !strings.Contains(err.Error(), "--r2-control-plane-binary") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "inventory") || strings.Contains(err.Error(), "OpenBao site root key") {
		t.Fatalf("bootstrap deploy reached remote-facing checks before local R2 validation: %v", err)
	}
}

func TestBootstrapDeployChecksOpenBaoBeforeRemoteAccess(t *testing.T) {
	root := t.TempDir()
	cloudflareBinary := filepath.Join(root, "cloudflare-control-plane")
	if err := os.WriteFile(cloudflareBinary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	r2Binary := filepath.Join(root, "cloudflare-r2-control-plane")
	if err := os.WriteFile(r2Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := RunBootstrapDeploy(context.Background(), BootstrapDeployOptions{
		Site:                 "gamma",
		SHA:                  "0123456789abcdef0123456789abcdef01234567",
		RepoRoot:             root,
		InventoryPath:        filepath.Join(root, "missing-inventory.ini"),
		CloudflareBinary:     cloudflareBinary,
		R2ControlPlaneBinary: r2Binary,
	})
	if err == nil || !strings.Contains(err.Error(), "--openbao-addr") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "inventory") || strings.Contains(err.Error(), "OpenBao site root key") {
		t.Fatalf("bootstrap deploy reached remote-facing checks before OpenBao validation: %v", err)
	}
}

func TestBootstrapR2ControlPlaneCommandUsesCredentialFiles(t *testing.T) {
	root := t.TempDir()
	r2Binary := filepath.Join(root, "cloudflare-r2-control-plane")
	if err := os.WriteFile(r2Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	cloudflareBinary := filepath.Join(root, "cloudflare-control-plane")
	if err := os.WriteFile(cloudflareBinary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	openBaoTokenFile := filepath.Join(root, "openbao-token")
	if err := os.WriteFile(openBaoTokenFile, []byte("openbao-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:                 "gamma",
		RepoRoot:             root,
		R2ControlPlaneBinary: r2Binary,
		CloudflareBinary:     cloudflareBinary,
		OpenBaoAddr:          "https://controller-openbao.internal",
		OpenBaoTokenFile:     openBaoTokenFile,
	})
	publisher := bootstrapPublisherCredential{
		AccessKeyID:     "publisher-token-id",
		SecretAccessKey: "publisher-secret",
		TokenID:         "publisher-token-id",
	}
	cmd, cleanup, err := startBootstrapR2ControlPlane(context.Background(), opts, "127.0.0.1:18732", "r2bootstrap_token", publisher)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{
		"--action=serve",
		"--site=gamma",
		"--repo-root=" + root,
		"--listen=127.0.0.1:18732",
		"--credential-source=" + bootstrapR2CredentialSource,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("R2 control-plane args missing %q:\n%s", want, args)
		}
	}
	for _, prefix := range []string{"--auth-token-file=", "--parent-access-key-id-file=", "--parent-secret-access-key-file="} {
		if argValue(cmd.Args, prefix) == "" {
			t.Fatalf("R2 control-plane args missing %s:\n%s", prefix, args)
		}
	}
	for path, want := range map[string]string{
		argValue(cmd.Args, "--auth-token-file="):               "r2bootstrap_token",
		argValue(cmd.Args, "--parent-access-key-id-file="):     "publisher-token-id",
		argValue(cmd.Args, "--parent-secret-access-key-file="): "publisher-secret",
	} {
		if got := strings.TrimSpace(readFile(t, path)); got != want {
			t.Fatalf("credential file %s = %q, want %q", path, got, want)
		}
	}
}

func TestCloudflareBootstrapPublisherArgsUseOpenBaoFiles(t *testing.T) {
	root := t.TempDir()
	opts := normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:              "gamma",
		RepoRoot:          root,
		OpenBaoAddr:       "https://controller-openbao.internal/",
		OpenBaoCACertFile: "ca.pem",
		OpenBaoTokenFile:  "token",
	})

	args := strings.Join(cloudflareOpenBaoArgs(opts), "\n")
	for _, want := range []string{
		"--openbao-addr=https://controller-openbao.internal",
		"--openbao-ca-cert=" + filepath.Join(root, "ca.pem"),
		"--openbao-token-file=" + filepath.Join(root, "token"),
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("OpenBao args missing %q:\n%s", want, args)
		}
	}
}

func TestBootstrapOpenBaoRootKeyPreflightChecksPresenceAndMode(t *testing.T) {
	cmd := sshCommand(context.Background(), inventoryTarget{
		Host: "gamma.example.test",
		User: "ubuntu",
		Port: 2222,
	}, openBaoRootKeyPreflightCommand())
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "test -s '/etc/verself/bootstrap/openbao-root.key'") {
		t.Fatalf("preflight command = %q, want root key presence check", args)
	}
	if !strings.Contains(args, "stat -c '%a' '/etc/verself/bootstrap/openbao-root.key'") {
		t.Fatalf("preflight command = %q, want mode check", args)
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

func argValue(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
