package nomadclient

import (
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestNextBatchEvaluationID(t *testing.T) {
	tests := []struct {
		name string
		eval *api.Evaluation
		want string
	}{
		{
			name: "empty",
			eval: &api.Evaluation{},
		},
		{
			name: "blocked eval",
			eval: &api.Evaluation{BlockedEval: "blocked"},
			want: "blocked",
		},
		{
			name: "next eval",
			eval: &api.Evaluation{NextEval: "next"},
			want: "next",
		},
		{
			name: "next eval wins",
			eval: &api.Evaluation{NextEval: "next", BlockedEval: "blocked"},
			want: "next",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextBatchEvaluationID(tt.eval); got != tt.want {
				t.Fatalf("nextBatchEvaluationID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBatchJobVersionComplete(t *testing.T) {
	tests := []struct {
		name    string
		allocs  []*api.AllocationListStub
		version uint64
		want    bool
	}{
		{
			name:    "complete current version",
			version: 3,
			allocs: []*api.AllocationListStub{{
				JobVersion:    3,
				DesiredStatus: api.AllocDesiredStatusRun,
				ClientStatus:  api.AllocClientStatusComplete,
			}},
			want: true,
		},
		{
			name:    "failed current version",
			version: 3,
			allocs: []*api.AllocationListStub{{
				JobVersion:    3,
				DesiredStatus: api.AllocDesiredStatusRun,
				ClientStatus:  api.AllocClientStatusFailed,
			}},
		},
		{
			name:    "stopped failed reschedule followed by complete",
			version: 3,
			allocs: []*api.AllocationListStub{
				{
					JobVersion:    3,
					DesiredStatus: api.AllocDesiredStatusStop,
					ClientStatus:  api.AllocClientStatusFailed,
				},
				{
					JobVersion:    3,
					DesiredStatus: api.AllocDesiredStatusRun,
					ClientStatus:  api.AllocClientStatusComplete,
				},
			},
			want: true,
		},
		{
			name:    "only previous version complete",
			version: 3,
			allocs: []*api.AllocationListStub{{
				JobVersion:    2,
				DesiredStatus: api.AllocDesiredStatusRun,
				ClientStatus:  api.AllocClientStatusComplete,
			}},
		},
		{
			name:    "current version still running",
			version: 3,
			allocs: []*api.AllocationListStub{{
				JobVersion:    3,
				DesiredStatus: api.AllocDesiredStatusRun,
				ClientStatus:  api.AllocClientStatusRunning,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := batchJobVersionComplete(tt.allocs, tt.version); got != tt.want {
				t.Fatalf("batchJobVersionComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceJobVersionHealthy(t *testing.T) {
	healthy := true
	unhealthy := false
	tests := []struct {
		name    string
		allocs  []*api.AllocationListStub
		version uint64
		want    bool
	}{
		{
			name:    "running current version",
			version: 7,
			allocs: []*api.AllocationListStub{{
				JobVersion:       7,
				DesiredStatus:    api.AllocDesiredStatusRun,
				ClientStatus:     api.AllocClientStatusRunning,
				DeploymentStatus: &api.AllocDeploymentStatus{Healthy: &healthy},
			}},
			want: true,
		},
		{
			name:    "pending current version",
			version: 7,
			allocs: []*api.AllocationListStub{{
				JobVersion:    7,
				DesiredStatus: api.AllocDesiredStatusRun,
				ClientStatus:  api.AllocClientStatusPending,
			}},
		},
		{
			name:    "unhealthy deployment status",
			version: 7,
			allocs: []*api.AllocationListStub{{
				JobVersion:       7,
				DesiredStatus:    api.AllocDesiredStatusRun,
				ClientStatus:     api.AllocClientStatusRunning,
				DeploymentStatus: &api.AllocDeploymentStatus{Healthy: &unhealthy},
			}},
		},
		{
			name:    "only old version running",
			version: 7,
			allocs: []*api.AllocationListStub{{
				JobVersion:       6,
				DesiredStatus:    api.AllocDesiredStatusRun,
				ClientStatus:     api.AllocClientStatusRunning,
				DeploymentStatus: &api.AllocDeploymentStatus{Healthy: &healthy},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceJobVersionHealthy(tt.allocs, tt.version); got != tt.want {
				t.Fatalf("serviceJobVersionHealthy() = %v, want %v", got, tt.want)
			}
		})
	}
}
