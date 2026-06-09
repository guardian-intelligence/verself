package deployengine

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestNomadApplyResultCountsSubmittedJobs(t *testing.T) {
	var result nomadApplyResult
	result.add(NomadRegisterResult{JobID: "web"})
	result.add(NomadRegisterResult{JobID: "billing"})
	if result.SubmittedJobs != 2 {
		t.Fatalf("submitted jobs = %d, want 2", result.SubmittedJobs)
	}
}

func TestApplyTaskSecretTemplateOwnershipSetsTaskUserOwnership(t *testing.T) {
	jobID := "api"
	groupName := "api-group"
	secretDestination := "secrets/api-token"
	localDestination := "local/config"
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{
			{
				Name: &groupName,
				Tasks: []*api.Task{
					{
						Name: "api",
						User: "api_user",
						Templates: []*api.Template{
							{DestPath: &secretDestination},
							{DestPath: &localDestination},
						},
					},
				},
			},
		},
	}

	err := applyTaskSecretTemplateOwnership(context.Background(), job, func(context.Context, string) (TaskUserIdentity, error) {
		return TaskUserIdentity{UID: 123, GID: 456}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	secretTemplate := job.TaskGroups[0].Tasks[0].Templates[0]
	if secretTemplate.Uid == nil || *secretTemplate.Uid != 123 {
		t.Fatalf("secret template uid = %v, want 123", secretTemplate.Uid)
	}
	if secretTemplate.Gid == nil || *secretTemplate.Gid != 456 {
		t.Fatalf("secret template gid = %v, want 456", secretTemplate.Gid)
	}
	localTemplate := job.TaskGroups[0].Tasks[0].Templates[1]
	if localTemplate.Uid != nil || localTemplate.Gid != nil {
		t.Fatalf("non-secret template ownership = uid:%v gid:%v, want unset", localTemplate.Uid, localTemplate.Gid)
	}
}

func TestApplyTaskSecretTemplateOwnershipRequiresTaskUser(t *testing.T) {
	jobID := "api"
	groupName := "api-group"
	secretDestination := "secrets/api-token"
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{
			{
				Name: &groupName,
				Tasks: []*api.Task{
					{
						Name:      "api",
						Templates: []*api.Template{{DestPath: &secretDestination}},
					},
				},
			},
		},
	}

	err := applyTaskSecretTemplateOwnership(context.Background(), job, nil)
	if err == nil || !strings.Contains(err.Error(), "task.user is empty") {
		t.Fatalf("error = %v, want empty task.user error", err)
	}
}

func TestApplyTaskSecretTemplateOwnershipRejectsExplicitMismatch(t *testing.T) {
	jobID := "api"
	groupName := "api-group"
	secretDestination := "secrets/api-token"
	uid := 999
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{
			{
				Name: &groupName,
				Tasks: []*api.Task{
					{
						Name: "api",
						User: "api_user",
						Templates: []*api.Template{
							{DestPath: &secretDestination, Uid: &uid},
						},
					},
				},
			},
		},
	}

	err := applyTaskSecretTemplateOwnership(context.Background(), job, func(context.Context, string) (TaskUserIdentity, error) {
		return TaskUserIdentity{UID: 123, GID: 456}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "has uid=999") {
		t.Fatalf("error = %v, want explicit uid mismatch", err)
	}
}

func TestApplyPodmanTaskUserIDsUsesResolvedNumericIdentity(t *testing.T) {
	jobID := "analytics-service"
	groupName := "analytics-service"
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{
			{
				Name: &groupName,
				Tasks: []*api.Task{
					{
						Name:   "analytics-service",
						Driver: "podman",
						User:   "analytics_service",
					},
					{
						Name:   "host-helper",
						Driver: "raw_exec",
						User:   "analytics_service",
					},
				},
			},
		},
	}

	err := applyPodmanTaskUserIDs(context.Background(), job, func(context.Context, string) (TaskUserIdentity, error) {
		return TaskUserIdentity{UID: 123, GID: 456}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := job.TaskGroups[0].Tasks[0].User; got != "123:456" {
		t.Fatalf("podman task user = %q, want numeric uid:gid", got)
	}
	if got := job.TaskGroups[0].Tasks[1].User; got != "analytics_service" {
		t.Fatalf("raw_exec task user = %q, want named user untouched", got)
	}
}

func TestApplyDeploymentMetadataStampsJob(t *testing.T) {
	jobID := "analytics-service"
	job := &api.Job{ID: &jobID}

	specSHA, err := nomadJobSpecSHA256(job)
	if err != nil {
		t.Fatal(err)
	}
	applyDeploymentMetadata(job, deployJobMetadata{
		DeployRunKey: "bootstrap_123",
		SHA:          strings.Repeat("a", 40),
	}, specSHA)

	if job.Meta["deploy_run_key"] != "bootstrap_123" {
		t.Fatalf("deploy_run_key = %q", job.Meta["deploy_run_key"])
	}
	if job.Meta["deploy_sha"] != strings.Repeat("a", 40) {
		t.Fatalf("deploy_sha = %q", job.Meta["deploy_sha"])
	}
	if job.Meta["spec_sha256"] != specSHA {
		t.Fatalf("spec_sha256 = %q, want %q", job.Meta["spec_sha256"], specSHA)
	}
}
