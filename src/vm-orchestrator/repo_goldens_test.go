package vmorchestrator

import (
	"strings"
	"testing"
)

func TestRepoURLForGuestRewritesHTTPToHostService(t *testing.T) {
	t.Parallel()

	got, err := repoURLForGuest("https://git.example.test/fixtures/app.git", "10.255.0.1", 18080)
	if err != nil {
		t.Fatalf("repoURLForGuest: %v", err)
	}
	if got != "http://10.255.0.1:18080/fixtures/app.git" {
		t.Fatalf("repo URL: got %q", got)
	}
}

func TestRepoURLWithoutCredentialsStripsUserinfoAndQuery(t *testing.T) {
	t.Parallel()

	got := repoURLWithoutCredentials("http://reader:secret@10.255.0.1:18080/fixtures/app.git?token=secret#frag")
	if got != "http://10.255.0.1:18080/fixtures/app.git" {
		t.Fatalf("repo URL without credentials: got %q", got)
	}
}

func TestRepoURLForGuestRejectsNonHTTP(t *testing.T) {
	t.Parallel()

	if _, err := repoURLForGuest("git@git.example.test:fixtures/app.git", "10.255.0.1", 18080); err == nil {
		t.Fatal("repoURLForGuest accepted an SSH-style repo URL")
	}
}

func TestWriteGuestEventShellAttr(t *testing.T) {
	t.Parallel()

	var script strings.Builder
	writeGuestEventShellAttr(&script, repoWarmCommitEvent, repoCommitSHAAttr, "$COMMIT_SHA")

	want := "emit_guest_event '{\"kind\":\"repo_warm.commit\",\"attrs\":{\"commit_sha\":\"'\"$COMMIT_SHA\"'\"}}'\n"
	if got := script.String(); got != want {
		t.Fatalf("guest event shell: got %q want %q", got, want)
	}
}

func TestBuildInVMRepoExecJobDoesNotMountOrFetchOnHost(t *testing.T) {
	t.Parallel()

	job, err := buildInVMRepoExecJob(JobConfig{
		JobID:          "11111111-1111-1111-1111-111111111111",
		PrepareCommand: []string{"npm", "ci"},
		RunCommand:     []string{"npm", "test"},
	}, "https://reader:secret@git.example.test/fixtures/app.git?token=secret", "refs/heads/main", "package-lock.json", "10.255.0.1", 18080)
	if err != nil {
		t.Fatalf("buildInVMRepoExecJob: %v", err)
	}
	if len(job.PrepareCommand) != 3 || job.PrepareCommand[0] != "sh" || job.PrepareCommand[1] != "-c" {
		t.Fatalf("unexpected prepare command: %#v", job.PrepareCommand)
	}
	script := job.PrepareCommand[2]
	for _, forbidden := range []string{"127.0.0.1", "route_localnet", "mount"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("prepare script contains forbidden host-coupled token %q:\n%s", forbidden, script)
		}
	}
	if !strings.Contains(script, "REPO_URL='http://reader:secret@10.255.0.1:18080/fixtures/app.git?token=secret'") {
		t.Fatalf("prepare script did not rewrite credentialed repo URL to host service plane:\n%s", script)
	}
	if !strings.Contains(script, "REPO_URL_NO_CREDENTIALS='http://10.255.0.1:18080/fixtures/app.git'") {
		t.Fatalf("prepare script did not compute a credential-stripped origin URL:\n%s", script)
	}
	if !strings.Contains(script, "git remote set-url origin \"$REPO_URL_NO_CREDENTIALS\"\n") {
		t.Fatalf("prepare script did not strip credentialed origin before repo commands:\n%s", script)
	}
	if !strings.Contains(script, "git fetch --depth 1 \"$REPO_URL\" 'refs/heads/main'\n") {
		t.Fatalf("prepare script did not fetch with the credentialed URL without persisting it:\n%s", script)
	}
	if !strings.Contains(script, "rm -f .git/FETCH_HEAD\nunset REPO_URL\n") {
		t.Fatalf("prepare script did not remove credential-bearing FETCH_HEAD before repo commands:\n%s", script)
	}
}
