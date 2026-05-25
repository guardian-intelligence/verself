package main

import (
	"reflect"
	"testing"
)

func TestTestMailboxDeliveryPrincipal(t *testing.T) {
	tests := map[string]string{
		"signup+agent@verself.sh":      "signup@verself.sh",
		"signup-e2e@verself.sh":        "signup-e2e@verself.sh",
		"signup.dotted@verself.sh":     "signup.dotted@verself.sh",
		"SIGNUP+AGENT@VERSELF.SH":      "signup@verself.sh",
		"signup+agent+two@verself.sh":  "signup@verself.sh",
		"signup_equals=tag@verself.sh": "signup_equals=tag@verself.sh",
	}
	for email, want := range tests {
		if got := testMailboxDeliveryPrincipal(email); got != want {
			t.Fatalf("testMailboxDeliveryPrincipal(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestTestMailboxDeliveryPrincipals(t *testing.T) {
	got := testMailboxDeliveryPrincipals([]string{
		"signup+agent@verself.sh",
		"signup+other@verself.sh",
		"signup-e2e@verself.sh",
	})
	want := []string{"signup-e2e@verself.sh", "signup@verself.sh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("testMailboxDeliveryPrincipals() = %#v, want %#v", got, want)
	}
}
