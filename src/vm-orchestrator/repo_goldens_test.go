package vmorchestrator

import "testing"

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

func TestBuildInVMRepoExecJobBuildsSupervisorOperation(t *testing.T) {
	t.Parallel()

	job, err := buildInVMRepoExecJob(JobConfig{
		JobID:          "11111111-1111-1111-1111-111111111111",
		PrepareCommand: []string{"npm", "ci"},
		RunCommand:     []string{"npm", "test"},
	}, "https://reader:secret@git.example.test/fixtures/app.git?token=secret", "refs/heads/main", "package-lock.json", "10.255.0.1", 18080)
	if err != nil {
		t.Fatalf("buildInVMRepoExecJob: %v", err)
	}
	if len(job.PrepareCommand) != 0 || len(job.RunCommand) != 0 {
		t.Fatalf("repo supervisor job should not carry shell phases: prepare=%#v run=%#v", job.PrepareCommand, job.RunCommand)
	}
	op := job.RepoOperation
	if op == nil {
		t.Fatal("repo supervisor operation is nil")
	}
	if op.Kind != "exec" {
		t.Fatalf("operation kind: got %q", op.Kind)
	}
	if op.RepoURL != "http://reader:secret@10.255.0.1:18080/fixtures/app.git?token=secret" {
		t.Fatalf("repo URL: got %q", op.RepoURL)
	}
	if op.OriginURL != "http://10.255.0.1:18080/fixtures/app.git" {
		t.Fatalf("origin URL: got %q", op.OriginURL)
	}
	if op.Ref != "refs/heads/main" {
		t.Fatalf("ref: got %q", op.Ref)
	}
	if op.LockfileRelPath != "package-lock.json" {
		t.Fatalf("lockfile: got %q", op.LockfileRelPath)
	}
	if len(op.UserPrepareCommand) != 2 || op.UserPrepareCommand[0] != "npm" || op.UserPrepareCommand[1] != "ci" {
		t.Fatalf("user prepare command: %#v", op.UserPrepareCommand)
	}
	if len(op.UserRunCommand) != 2 || op.UserRunCommand[0] != "npm" || op.UserRunCommand[1] != "test" {
		t.Fatalf("user run command: %#v", op.UserRunCommand)
	}
}
