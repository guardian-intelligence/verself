package main

import (
	"slices"
	"testing"

	workloadauth "github.com/verself/service-runtime/workload"
)

func TestBillingInternalPeerServices(t *testing.T) {
	got := billingInternalPeerServices()
	want := []string{
		workloadauth.ServiceIAM,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceSecrets,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("billing internal peer services = %v, want %v", got, want)
	}
}
