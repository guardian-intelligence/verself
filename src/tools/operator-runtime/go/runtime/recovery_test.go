package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

const recoveryInventory = `[workers]
vs-dev-w0 ansible_host=access.verself.sh verself_recovery_ssh_host=10.66.66.1 verself_recovery_ssh_port=2222 verself_recovery_ssh_user=ubuntu

[infra]
vs-dev-w0 ansible_host=access.verself.sh verself_recovery_ssh_host=10.66.66.1 verself_recovery_ssh_port=2222 verself_recovery_ssh_user=ubuntu

[all:vars]
ansible_user=ubuntu@prod
`

func writeInventory(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inventory.ini")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	return path
}

func TestLoadInfraTargetParsesRecoveryFacts(t *testing.T) {
	target, err := LoadInfraTarget(writeInventory(t, recoveryInventory))
	if err != nil {
		t.Fatalf("LoadInfraTarget: %v", err)
	}
	if target.Host != "access.verself.sh" || target.User != "ubuntu@prod" {
		t.Fatalf("primary target = %q@%q, want ubuntu@prod@access.verself.sh", target.User, target.Host)
	}
	if target.RecoveryHost != "10.66.66.1" || target.RecoveryUser != "ubuntu" || target.RecoveryPort != 2222 {
		t.Fatalf("recovery facts = %q@%q:%d, want ubuntu@10.66.66.1:2222", target.RecoveryUser, target.RecoveryHost, target.RecoveryPort)
	}
}

func TestRecoverySelectsWireGuardTargetAndPort(t *testing.T) {
	target, err := LoadInfraTarget(writeInventory(t, recoveryInventory))
	if err != nil {
		t.Fatalf("LoadInfraTarget: %v", err)
	}
	recovery, ok := target.Recovery()
	if !ok {
		t.Fatal("Recovery() = false, want a recovery target")
	}
	if recovery.Host != "10.66.66.1" || recovery.User != "ubuntu" {
		t.Fatalf("recovery target = %q@%q, want ubuntu@10.66.66.1", recovery.User, recovery.Host)
	}
	// A plain user makes the WireGuard recovery port the first dial candidate,
	// so the recovery path never lands on the Pomerium SSH listener.
	if ports := recovery.SSHPorts(); len(ports) == 0 || ports[0] != 2222 {
		t.Fatalf("recovery SSHPorts() = %v, want 2222 first", ports)
	}
}

func TestRecoveryAbsentWhenNotDeclared(t *testing.T) {
	body := "[infra]\nvs-dev-w0 ansible_host=access.verself.sh\n[all:vars]\nansible_user=ubuntu@prod\n"
	target, err := LoadInfraTarget(writeInventory(t, body))
	if err != nil {
		t.Fatalf("LoadInfraTarget: %v", err)
	}
	if _, ok := target.Recovery(); ok {
		t.Fatal("Recovery() = true with no verself_recovery_ssh_host declared")
	}
}

func TestResolveInfraTargetUsesRecoveryEnvironment(t *testing.T) {
	root := t.TempDir()
	inventory := filepath.Join(root, "src", "sites", "prod", "inventory.ini")
	if err := os.MkdirAll(filepath.Dir(inventory), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inventory, []byte(recoveryInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VERSELF_OPERATOR_SSH_TRANSPORT", "recovery")
	target, err := resolveInfraTarget(root, "prod", Options{UseRecovery: operatorUseRecoveryEnv()})
	if err != nil {
		t.Fatalf("resolveInfraTarget: %v", err)
	}
	if target.Host != "10.66.66.1" || target.User != "ubuntu" {
		t.Fatalf("target = %q@%q, want ubuntu@10.66.66.1", target.User, target.Host)
	}
}

func TestFindSignInURL(t *testing.T) {
	url := findSignInURL("Please sign in to continue", "https://access.verself.sh/.pomerium/sign_in?user_code=abc", "")
	if url != "https://access.verself.sh/.pomerium/sign_in?user_code=abc" {
		t.Fatalf("findSignInURL = %q, want the device sign-in URL", url)
	}
	if got := findSignInURL("no url here", "plain text"); got != "" {
		t.Fatalf("findSignInURL = %q, want empty when no URL present", got)
	}
}
