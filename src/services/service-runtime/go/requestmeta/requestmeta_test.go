package requestmeta

import (
	"net/http"
	"testing"
)

func TestFromValuesTrustsOnlyCanonicalEdgeHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderClientIP, "2001:db8::1")
	headers.Set(HeaderEdgePeerIP, "198.51.100.10")
	headers.Set(HeaderClientIPSource, SourceTrustedEdge)
	headers.Set("CF-Connecting-IP", "203.0.113.66")
	headers.Set("X-Forwarded-For", "203.0.113.67")

	got := FromValues(headers.Get, "10.0.0.10:443")
	if got.ClientIP != "2001:db8::1" {
		t.Fatalf("ClientIP = %q, want canonical edge value", got.ClientIP)
	}
	if got.EdgePeerIP != "198.51.100.10" {
		t.Fatalf("EdgePeerIP = %q, want canonical edge peer", got.EdgePeerIP)
	}
	if !got.Trusted {
		t.Fatal("Trusted = false, want true")
	}
	if len(got.RejectedHeaders) != 1 || got.RejectedHeaders[0] != "CF-Connecting-IP" {
		t.Fatalf("RejectedHeaders = %#v, want only raw vendor header", got.RejectedHeaders)
	}
}

func TestFromValuesFallsBackAndRejectsSpoofableHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("CF-Connecting-IP", "203.0.113.66")
	headers.Set("X-Forwarded-For", "203.0.113.67")
	headers.Set("Forwarded", "for=203.0.113.68")

	got := FromValues(headers.Get, "10.0.0.10:443")
	if got.ClientIP != "10.0.0.10" {
		t.Fatalf("ClientIP = %q, want remote address fallback", got.ClientIP)
	}
	if got.Trusted {
		t.Fatal("Trusted = true, want false")
	}
	want := []string{"CF-Connecting-IP", "Forwarded", "X-Forwarded-For"}
	if len(got.RejectedHeaders) != len(want) {
		t.Fatalf("RejectedHeaders = %#v, want %#v", got.RejectedHeaders, want)
	}
	for i := range want {
		if got.RejectedHeaders[i] != want[i] {
			t.Fatalf("RejectedHeaders = %#v, want %#v", got.RejectedHeaders, want)
		}
	}
}

func TestFromValuesRejectsInvalidCanonicalHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set(HeaderClientIP, "203.0.113.1, 198.51.100.2")
	headers.Set("X-Real-IP", "203.0.113.1")

	got := FromValues(headers.Get, "[2001:db8::99]:1234")
	if got.ClientIP != "2001:db8::99" {
		t.Fatalf("ClientIP = %q, want normalized remote fallback", got.ClientIP)
	}
	if got.Trusted {
		t.Fatal("Trusted = true, want false")
	}
	want := []string{"X-Real-IP", HeaderClientIP}
	if len(got.RejectedHeaders) != len(want) {
		t.Fatalf("RejectedHeaders = %#v, want %#v", got.RejectedHeaders, want)
	}
	for i := range want {
		if got.RejectedHeaders[i] != want[i] {
			t.Fatalf("RejectedHeaders = %#v, want %#v", got.RejectedHeaders, want)
		}
	}
}
