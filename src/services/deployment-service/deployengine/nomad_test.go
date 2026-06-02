package deployengine

import (
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

func TestBlockingRuntimeBatchJobsIgnoresOptionalBatchChains(t *testing.T) {
	batchType := "batch"
	serviceType := "service"
	jobs := []nomadJob{
		{
			JobID:    "clickhouse-host-install",
			Provides: []string{"clickhouse:host-tools"},
			Job:      &api.Job{Type: &batchType},
		},
		{
			JobID:    "clickhouse-migrations",
			Provides: []string{"clickhouse:migrations"},
			Requires: []string{"clickhouse:host-tools"},
			Job:      &api.Job{Type: &batchType},
		},
		{
			JobID:    "deployment-service",
			Requires: []string{"postgres:server"},
			Job:      &api.Job{Type: &serviceType},
		},
	}

	blocking := blockingRuntimeBatchJobs(jobs)
	if len(blocking) != 0 {
		t.Fatalf("blocking batch jobs = %+v, want none because no service requires clickhouse", blocking)
	}
}

func TestBlockingRuntimeBatchJobsBlocksTransitiveBatchPrereqs(t *testing.T) {
	batchType := "batch"
	serviceType := "service"
	jobs := []nomadJob{
		{
			JobID:    "postgres-host-install",
			Provides: []string{"postgres:host-tools"},
			Job:      &api.Job{Type: &batchType},
		},
		{
			JobID:    "postgres-migrations",
			Provides: []string{"postgres:schema"},
			Requires: []string{"postgres:host-tools"},
			Job:      &api.Job{Type: &batchType},
		},
		{
			JobID:    "api",
			Requires: []string{"postgres:schema"},
			Job:      &api.Job{Type: &serviceType},
		},
	}

	blocking := blockingRuntimeBatchJobs(jobs)
	if len(blocking) != 2 {
		t.Fatalf("blocking batch jobs = %+v, want transitive postgres batches", blocking)
	}
	for _, jobID := range []string{"postgres-host-install", "postgres-migrations"} {
		if !blocking[jobID] {
			t.Fatalf("blocking[%s] = false, want true", jobID)
		}
	}
	if blocking["api"] {
		t.Fatalf("non-batch api was marked blocking")
	}
}

func TestBlockingRuntimeBatchJobsAllowsBatchToDependOnService(t *testing.T) {
	batchType := "batch"
	serviceType := "service"
	jobs := []nomadJob{
		{
			JobID:    "zitadel",
			Provides: []string{"zitadel:http"},
			Job:      &api.Job{Type: &serviceType},
		},
		{
			JobID:    "auth-control-plane",
			Provides: []string{"auth:control-plane"},
			Requires: []string{"zitadel:http"},
			Job:      &api.Job{Type: &batchType},
		},
		{
			JobID:    "iam-service",
			Requires: []string{"auth:control-plane"},
			Job:      &api.Job{Type: &serviceType},
		},
	}

	blocking := blockingRuntimeBatchJobs(jobs)
	if !blocking["auth-control-plane"] {
		t.Fatalf("auth-control-plane should block dependent services")
	}
	if blocking["zitadel"] {
		t.Fatalf("service producer zitadel was marked as a blocking batch")
	}
}

func TestOrderNomadComponentsRespectsControlPlaneResourceChain(t *testing.T) {
	components := []nomadComponentDescriptor{
		{
			Label:       "//src/services/deployment-service:nomad_component",
			Component:   "deployment_service",
			DeployPhase: deployPhasePlatform,
			JobID:       "deployment-service",
			Requires: []string{
				"cloudflare:r2-control-plane",
				"nomad:job:substrate-control-plane",
			},
		},
		{
			Label:       "//src/integrations/cloudflare/r2-control-plane:nomad_component",
			Component:   "cloudflare_r2_control_plane",
			DeployPhase: deployPhasePlatform,
			JobID:       "cloudflare-r2-control-plane",
			Provides:    []string{"cloudflare:r2-control-plane"},
			Requires:    []string{"nomad:job:substrate-control-plane"},
		},
		{
			Label:       "//src/infrastructure-components/substrate-control-plane:nomad_component",
			Component:   "substrate_control_plane",
			DeployPhase: deployPhasePlatform,
			JobID:       "substrate-control-plane",
			Provides: []string{
				"nomad:job:substrate-control-plane",
				"substrate:control-plane",
			},
		},
	}

	ordered, err := orderNomadComponents(components)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for index, component := range ordered {
		positions[component.JobID] = index
	}
	if positions["substrate-control-plane"] > positions["cloudflare-r2-control-plane"] {
		t.Fatalf("substrate-control-plane ordered after cloudflare-r2-control-plane: %+v", positions)
	}
	if positions["cloudflare-r2-control-plane"] > positions["deployment-service"] {
		t.Fatalf("cloudflare-r2-control-plane ordered after deployment-service: %+v", positions)
	}
}
