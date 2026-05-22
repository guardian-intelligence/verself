package workload

import (
	"testing"
	"time"
)

func TestServiceClientTimeoutsWithDefaults(t *testing.T) {
	got := (ServiceClientTimeouts{ResponseHeader: 30 * time.Second, Total: 45 * time.Second}).withDefaults()
	if got.Dial != serviceClientDialTimeout {
		t.Fatalf("Dial = %s, want %s", got.Dial, serviceClientDialTimeout)
	}
	if got.TLSHandshake != serviceClientTLSHandshakeTimeout {
		t.Fatalf("TLSHandshake = %s, want %s", got.TLSHandshake, serviceClientTLSHandshakeTimeout)
	}
	if got.ResponseHeader != 30*time.Second {
		t.Fatalf("ResponseHeader = %s, want 30s", got.ResponseHeader)
	}
	if got.Total != 45*time.Second {
		t.Fatalf("Total = %s, want 45s", got.Total)
	}
}
