package main

import "testing"

func TestEdgeRuntimeCommandWhitelistedViews(t *testing.T) {
	tests := []struct {
		name string
		opts edgeRuntimeOptions
		want string
	}{
		{
			name: "info text",
			opts: edgeRuntimeOptions{show: "info", format: "text"},
			want: "show info",
		},
		{
			name: "info json",
			opts: edgeRuntimeOptions{show: "info", format: "json"},
			want: "show info json",
		},
		{
			name: "stat typed",
			opts: edgeRuntimeOptions{show: "stat", format: "typed"},
			want: "show stat typed",
		},
		{
			name: "sni",
			opts: edgeRuntimeOptions{show: "sni", format: "text"},
			want: "show ssl sni",
		},
		{
			name: "all tables",
			opts: edgeRuntimeOptions{show: "table", format: "text"},
			want: "show table",
		},
		{
			name: "named table",
			opts: edgeRuntimeOptions{show: "table", format: "text", table: "be_edge_public_rates"},
			want: "show table be_edge_public_rates",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := edgeRuntimeCommand(tt.opts)
			if err != nil {
				t.Fatalf("edgeRuntimeCommand() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("edgeRuntimeCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEdgeRuntimeCommandRejectsUnsafeInputs(t *testing.T) {
	tests := []edgeRuntimeOptions{
		{show: "raw", format: "text"},
		{show: "info", format: "yaml"},
		{show: "sni", format: "json"},
		{show: "table", format: "text", table: "be_edge_public_rates;shutdown sessions"},
	}
	for _, tt := range tests {
		if got, err := edgeRuntimeCommand(tt); err == nil {
			t.Fatalf("edgeRuntimeCommand(%+v) = %q, want error", tt, got)
		}
	}
}
