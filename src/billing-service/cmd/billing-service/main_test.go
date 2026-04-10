package main

import "testing"

func TestIsUnauthenticatedBillingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "/healthz", want: true},
		{path: "/readyz", want: true},
		{path: "/openapi.yaml", want: true},
		{path: "/webhooks/stripe", want: true},
		{path: "/internal/billing/v1/orgs/123/balance", want: true},
		{path: "/internal/billing/v1/checkout", want: true},
		{path: "/internal/billing/v1/subscribe", want: true},
		{path: "/internal/billing/v1/reserve", want: false},
		{path: "/internal/billing/v1/settle", want: false},
		{path: "/internal/billing/v1/void", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := isUnauthenticatedBillingPath(tt.path); got != tt.want {
				t.Fatalf("isUnauthenticatedBillingPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
