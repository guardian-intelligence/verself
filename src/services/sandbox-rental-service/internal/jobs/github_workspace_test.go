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
	want := "/workspace/.verself/actions-runner/_work/verself/verself"
	if got != want {
		t.Fatalf("githubWorkspacePath = %q, want %q", got, want)
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
