package nomadlint

import (
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
)

func TestLintRejectsTaskStatesWithEphemeralWorkerTask(t *testing.T) {
	health := "task_states"
	job := testJob("service", &api.TaskGroup{
		Name: str("worker"),
		Meta: map[string]string{metaGroupKind: GroupKindWorker},
		Tasks: []*api.Task{
			{Name: "init", Lifecycle: &api.TaskLifecycle{Hook: api.TaskLifecycleHookPrestart}},
			{Name: "worker"},
		},
		Update: &api.UpdateStrategy{HealthCheck: &health},
	})

	result := Lint(job)
	assertRule(t, result, "nomad.health.task_states_with_ephemeral_tasks")
}

func TestLintRequiresModeledLoopbackFallback(t *testing.T) {
	health := "checks"
	job := testJob("service", &api.TaskGroup{
		Name: str("service"),
		Meta: map[string]string{metaGroupKind: GroupKindService},
		Tasks: []*api.Task{{
			Name: "service",
			Services: []*api.Service{{
				Name:   "service-http",
				Checks: []api.ServiceCheck{{Name: "ready", Type: "http"}},
			}},
			Templates: []*api.Template{{
				DestPath:     str("secrets/upstreams.env"),
				EmbeddedTmpl: str(`UPSTREAM={{ with nomadService "missing" }}ok{{ else }}127.0.0.1:1{{ end }}`),
			}},
		}},
		Update: &api.UpdateStrategy{HealthCheck: &health},
	})

	result := Lint(job)
	assertRule(t, result, "nomad.template.loopback_blackhole_fallback")
}

func TestLintAllowsModeledLoopbackFallback(t *testing.T) {
	health := "checks"
	job := testJob("service", &api.TaskGroup{
		Name: str("service"),
		Meta: map[string]string{
			metaGroupKind:              GroupKindService,
			metaTemplateFallback:       "blackhole",
			metaTemplateFallbackReason: "bootstrap dependency gates readiness",
		},
		Tasks: []*api.Task{{
			Name: "service",
			Services: []*api.Service{{
				Name:   "service-http",
				Checks: []api.ServiceCheck{{Name: "ready", Type: "http"}},
			}},
			Templates: []*api.Template{{
				DestPath:     str("secrets/upstreams.env"),
				EmbeddedTmpl: str(`UPSTREAM={{ with nomadService "missing" }}ok{{ else }}127.0.0.1:1{{ end }}`),
			}},
		}},
		Update: &api.UpdateStrategy{HealthCheck: &health},
	})

	result := Lint(job)
	if result.Failed() {
		t.Fatalf("expected no errors, got %+v", result.Findings)
	}
}

func TestLintRequiresExplicitServiceUpdateHealth(t *testing.T) {
	job := testJob("service", &api.TaskGroup{
		Name: str("service"),
		Meta: map[string]string{metaGroupKind: GroupKindService},
		Tasks: []*api.Task{{
			Name: "service",
			Services: []*api.Service{{
				Name:   "service-http",
				Checks: []api.ServiceCheck{{Name: "ready", Type: "http"}},
			}},
		}},
	})

	result := Lint(job)
	assertRule(t, result, "nomad.health.service_requires_explicit_update")
}

func TestLintRejectsReservationAboveNodeCapacity(t *testing.T) {
	health := "task_states"
	cpu := 2000
	memory := 4096
	job := testJob("service", &api.TaskGroup{
		Name: str("worker"),
		Meta: map[string]string{metaGroupKind: GroupKindWorker},
		Tasks: []*api.Task{{
			Name:      "worker",
			Resources: &api.Resources{CPU: &cpu, MemoryMB: &memory},
		}},
		Update: &api.UpdateStrategy{HealthCheck: &health},
	})

	result := LintWithCapacity(job, []NodeCapacity{{
		NodeID:   "node-1",
		CPU:      1000,
		MemoryMB: 8192,
		DiskMB:   10_000,
	}})
	assertRule(t, result, "nomad.resources.cpu_exceeds_node_class")
}

func TestLintRejectsDeadlineOrder(t *testing.T) {
	health := "checks"
	healthy := time.Minute
	progress := time.Minute
	job := testJob("service", &api.TaskGroup{
		Name: str("service"),
		Meta: map[string]string{metaGroupKind: GroupKindService},
		Tasks: []*api.Task{{
			Name: "service",
			Services: []*api.Service{{
				Name:   "service-http",
				Checks: []api.ServiceCheck{{Name: "ready", Type: "http"}},
			}},
		}},
		Update: &api.UpdateStrategy{HealthCheck: &health, HealthyDeadline: &healthy, ProgressDeadline: &progress},
	})

	result := Lint(job)
	assertRule(t, result, "nomad.update.progress_deadline_order")
}

func assertRule(t *testing.T, result Result, ruleID string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.RuleID == ruleID {
			return
		}
	}
	t.Fatalf("missing rule %s in %+v", ruleID, result.Findings)
}

func testJob(kind string, groups ...*api.TaskGroup) *api.Job {
	return &api.Job{ID: str("test"), Type: str(kind), TaskGroups: groups}
}

func str(value string) *string {
	return &value
}
