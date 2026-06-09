package nomadclient

import (
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestDeploymentHasUnhealthyOnlyTaskGroup(t *testing.T) {
	deployment := &api.Deployment{
		TaskGroups: map[string]*api.DeploymentState{
			"api": {HealthyAllocs: 0, UnhealthyAllocs: 1},
		},
	}
	if !deploymentHasUnhealthyOnlyTaskGroup(deployment) {
		t.Fatal("deployment should be unhealthy-only")
	}

	deployment.TaskGroups["api"].HealthyAllocs = 1
	if deploymentHasUnhealthyOnlyTaskGroup(deployment) {
		t.Fatal("deployment with a healthy allocation should keep waiting")
	}
}
