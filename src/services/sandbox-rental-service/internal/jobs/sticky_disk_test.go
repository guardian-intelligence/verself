package jobs

import "testing"

func TestResolveStickyDiskPathUsesGitHubWorkspace(t *testing.T) {
	got, err := resolveStickyDiskPath("src/frontends/viteplus-monorepo/node_modules", "guardian-intelligence/verself")
	if err != nil {
		t.Fatalf("resolveStickyDiskPath returned error: %v", err)
	}
	want := "/workspace/verself/verself/src/frontends/viteplus-monorepo/node_modules"
	if got != want {
		t.Fatalf("resolveStickyDiskPath = %q, want %q", got, want)
	}
}

func TestResolveStickyDiskPathKeepsRunnerHome(t *testing.T) {
	got, err := resolveStickyDiskPath("~/.npm", "guardian-intelligence/verself")
	if err != nil {
		t.Fatalf("resolveStickyDiskPath returned error: %v", err)
	}
	want := "/home/runner/.npm"
	if got != want {
		t.Fatalf("resolveStickyDiskPath = %q, want %q", got, want)
	}
}

func TestResolveStickyDiskPathRejectsRelativePathWithoutRepository(t *testing.T) {
	if _, err := resolveStickyDiskPath("node_modules", "verself"); err == nil {
		t.Fatal("resolveStickyDiskPath returned nil error")
	}
}

func TestSetupNodeDeclarationsDeriveConservativeStickyKeys(t *testing.T) {
	workflow := []byte(`jobs:
  runner-canary:
    runs-on: verself-4vcpu-ubuntu-2404
    steps:
      - uses: guardian-intelligence/verself/.github/actions/setup-node@main
        with:
          node-version: 24
          package-manager: pnpm
          working-directory: src/frontends/viteplus-monorepo
          cache: true
          node-modules: true
`)
	files := map[string][]byte{
		"src/frontends/viteplus-monorepo/pnpm-lock.yaml": []byte("lockfileVersion: '9.0'\n"),
		"src/frontends/viteplus-monorepo/package.json":   []byte(`{"packageManager":"pnpm@10.33.0"}`),
	}
	decls, err := stickyDiskDeclarationsForJob(workflow, "runner-canary", "verself-4vcpu-ubuntu-2404", "guardian-intelligence/verself", func(path string) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, ErrGitHubContentNotFound
		}
		return data, nil
	})
	if err != nil {
		t.Fatalf("stickyDiskDeclarationsForJob returned error: %v", err)
	}
	if len(decls) != 2 {
		t.Fatalf("declaration count = %d, want 2", len(decls))
	}
	lockHash := sha256Hex(files["src/frontends/viteplus-monorepo/pnpm-lock.yaml"])
	wantStoreKey := "setup-node:v1:repo=guardian-intelligence/verself:runner=verself-4vcpu-ubuntu-2404:node=24:pm=pnpm@10.33.0:workdir=src/frontends/viteplus-monorepo:lock=" + lockHash + ":store"
	wantModulesKey := "setup-node:v1:repo=guardian-intelligence/verself:runner=verself-4vcpu-ubuntu-2404:node=24:pm=pnpm@10.33.0:workdir=src/frontends/viteplus-monorepo:lock=" + lockHash + ":node_modules"
	if decls[0].Key != wantStoreKey || decls[0].Path != "~/.pnpm-store" {
		t.Fatalf("store declaration = %#v", decls[0])
	}
	if decls[1].Key != wantModulesKey || decls[1].Path != "src/frontends/viteplus-monorepo/node_modules" {
		t.Fatalf("node_modules declaration = %#v", decls[1])
	}
}
