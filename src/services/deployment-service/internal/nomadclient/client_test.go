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

func TestNodeHasVaultVersion(t *testing.T) {
	if nodeHasVaultVersion(&api.Node{}) {
		t.Fatal("empty node should not have Vault integration")
	}
	if nodeHasVaultVersion(&api.Node{Attributes: map[string]string{"plugins.secrets.vault.version": "  "}}) {
		t.Fatal("blank plugins.secrets.vault.version should not have Vault integration")
	}
	if nodeHasVaultVersion(&api.Node{Attributes: map[string]string{"vault.version": "1.0.0"}}) {
		t.Fatal("legacy vault.version key should not mark Vault integration ready")
	}
	if !nodeHasVaultVersion(&api.Node{Attributes: map[string]string{"plugins.secrets.vault.version": "1.0.0"}}) {
		t.Fatal("plugins.secrets.vault.version should mark Vault integration ready")
	}
}
