package nomadclient

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"
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

func TestIsTransientNomadQueryError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil"},
		{name: "plain error", err: fmt.Errorf("nomad rejected query")},
		{name: "eof", err: io.EOF, want: true},
		{name: "wrapped eof", err: fmt.Errorf("query: %w", io.EOF), want: true},
		{name: "url eof", err: &url.Error{Op: "Get", URL: "http://127.0.0.1/v1/evaluation/id/allocations", Err: io.EOF}, want: true},
		{name: "connection reset", err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}, want: true},
		{name: "connection refused", err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientNomadQueryError(tt.err); got != tt.want {
				t.Fatalf("isTransientNomadQueryError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
