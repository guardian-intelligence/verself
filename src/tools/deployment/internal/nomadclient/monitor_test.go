package nomadclient

import (
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestSpecExecutionMode(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		want ExecutionMode
	}{
		{name: "default service", spec: &Spec{Job: &api.Job{}}, want: ExecutionModeServiceRollout},
		{name: "batch run", spec: &Spec{Job: &api.Job{Type: stringPtr("batch")}}, want: ExecutionModeBatchRun},
		{name: "dispatch template", spec: &Spec{Job: &api.Job{Type: stringPtr("batch"), ParameterizedJob: &api.ParameterizedJobConfig{}}}, want: ExecutionModeDispatchTemplate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.ExecutionMode(); got != tt.want {
				t.Fatalf("ExecutionMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

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

func stringPtr(value string) *string {
	return &value
}
