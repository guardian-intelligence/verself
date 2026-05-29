package nomadclient

import (
	"context"
	"errors"
	"io"
	"net/url"
	"testing"
	"time"

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

func TestIsRetryableNomadQueryError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "url eof",
			err:  &url.Error{Op: "Get", URL: "http://127.0.0.1:4646/v1/evaluation/e/allocations", Err: io.EOF},
			want: true,
		},
		{
			name: "connection reset",
			err:  errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: read: connection reset by peer"),
			want: true,
		},
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
		},
		{
			name: "scheduler failure",
			err:  errors.New("allocation failed: Resources exhausted on 1 nodes"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableNomadQueryError(tt.err); got != tt.want {
				t.Fatalf("isRetryableNomadQueryError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestMonitorQueryRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 250 * time.Millisecond},
		{attempt: 1, want: 250 * time.Millisecond},
		{attempt: 2, want: 500 * time.Millisecond},
		{attempt: 4, want: 2 * time.Second},
		{attempt: 8, want: 2 * time.Second},
	}
	for _, tt := range tests {
		if got := monitorQueryRetryDelay(tt.attempt); got != tt.want {
			t.Fatalf("monitorQueryRetryDelay(%d) = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}
