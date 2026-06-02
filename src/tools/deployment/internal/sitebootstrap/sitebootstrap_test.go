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

func TestBootstrapR2ControlPlaneCommandUsesInMemoryPublisher(t *testing.T) {
	root := t.TempDir()
	r2Binary := filepath.Join(root, "cloudflare-r2-control-plane")
	if err := os.WriteFile(r2Binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	cloudflareBinary := filepath.Join(root, "cloudflare-control-plane")
	if err := os.WriteFile(cloudflareBinary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	opts := normalizeBootstrapDeployOptions(BootstrapDeployOptions{
		Site:                 "gamma",
		RepoRoot:             root,
		R2ControlPlaneBinary: r2Binary,
		CloudflareBinary:     cloudflareBinary,
	})
	publisher := bootstrapPublisherCredential{
		AccessKeyID:     "publisher-token-id",
		SecretAccessKey: "publisher-secret",
		TokenID:         "publisher-token-id",
	}
	cmd, err := startBootstrapR2ControlPlane(context.Background(), opts, "127.0.0.1:18732", "r2bootstrap_token", publisher)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{
		"--action=serve",
		"--site=gamma",
		"--repo-root=" + root,
		"--listen=127.0.0.1:18732",
		"--auth-token-env=" + bootstrapR2AuthTokenEnv,
		"--credential-source=" + bootstrapR2CredentialSource,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("R2 control-plane args missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "--credentials-file") {
		t.Fatalf("R2 bootstrap command still depends on a credential file:\n%s", args)
	}
	for _, forbidden := range []string{"account-admin", "openbao", "BAO_", "VAULT_"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("R2 bootstrap command contains forbidden authority detail %q:\n%s", forbidden, args)
		}
	}
	if strings.Contains(args, "-R") {
		t.Fatalf("R2 bootstrap command still contains temporary artifact tunnel details:\n%s", args)
	}
	if !envContains(cmd.Env, bootstrapR2AuthTokenEnv+"=r2bootstrap_token") {
		t.Fatalf("R2 control-plane command did not carry bootstrap auth token env: %v", cmd.Env)
	}
	if !envContains(cmd.Env, bootstrapR2AccessKeyIDEnv+"=publisher-token-id") {
		t.Fatalf("R2 control-plane command did not carry publisher token id env: %v", cmd.Env)
	}
	if !envContains(cmd.Env, bootstrapR2SecretAccessEnv+"=publisher-secret") {
		t.Fatalf("R2 control-plane command did not carry publisher secret env: %v", cmd.Env)
	}
}

func TestBootstrapOpenBaoRootKeyPreflightChecksPresenceWithoutPrintingSecrets(t *testing.T) {
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
	if strings.Contains(args, "cat /etc/verself/bootstrap/openbao-root.key") {
		t.Fatalf("preflight command prints root key: %q", args)
	}
	if strings.Contains(args, "set -x") {
		t.Fatalf("preflight command enables shell tracing: %q", args)
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

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func envContains(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
