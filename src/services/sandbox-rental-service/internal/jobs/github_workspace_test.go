package jobs

import (
	"strings"
	"testing"
)

func TestGitHubWorkspacePathUsesActionsRunnerWorkLayout(t *testing.T) {
	got, err := githubWorkspacePath("guardian-intelligence/verself")
	if err != nil {
		t.Fatalf("githubWorkspacePath returned error: %v", err)
	}
	want := "/workspace/.verself/actions-runner/_work"
	if got != want {
		t.Fatalf("githubWorkspacePath = %q, want %q", got, want)
	}

	got, err = githubRunnerWorkspacePath("guardian-intelligence/verself")
	if err != nil {
		t.Fatalf("githubRunnerWorkspacePath returned error: %v", err)
	}
	want = "/tmp/verself-actions-runner/_work/verself/verself"
	if got != want {
		t.Fatalf("githubRunnerWorkspacePath = %q, want %q", got, want)
	}
}

func TestGitHubWorkspacePathRejectsInvalidRepository(t *testing.T) {
	for _, repo := range []string{"verself", "guardian-intelligence/", "guardian-intelligence/../verself", "guardian-intelligence/verse lf"} {
		if _, err := githubWorkspacePath(repo); err == nil {
			t.Fatalf("githubWorkspacePath(%q) returned nil error", repo)
		}
	}
}

func TestRunnerCommandsKeepToolCacheOutsideDurableWorkspaceParent(t *testing.T) {
	for name, command := range map[string]string{
		"github":  githubRunnerCommand(),
		"forgejo": forgejoRunnerCommand(),
	} {
		if strings.Contains(command, "/workspace/.verself/hostedtoolcache") {
			t.Fatalf("%s command places tool cache under durable workspace parent", name)
		}
		if !strings.Contains(command, `tool_cache="`+runnerToolCacheDir+`"`) {
			t.Fatalf("%s command does not use runnerToolCacheDir", name)
		}
	}
}

func TestGitHubRunnerCommandKeepsRuntimeOutsideDurableWorkspace(t *testing.T) {
	command := githubRunnerCommand()
	if strings.Contains(command, `runtime_dir="/workspace`) {
		t.Fatalf("github runner runtime is under durable workspace")
	}
	for _, want := range []string{
		`runtime_dir="` + githubRunnerRuntimeDir + `"`,
		`durable_work_dir="` + githubRunnerDurableWorkDir + `"`,
		`ln -s "$durable_work_dir" "$runtime_dir/_work"`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("github runner command missing %q", want)
		}
	}
}
