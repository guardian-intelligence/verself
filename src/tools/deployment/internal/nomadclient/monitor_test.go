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
