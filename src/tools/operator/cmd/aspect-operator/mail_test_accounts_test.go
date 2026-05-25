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

func TestTestMailboxPrincipalEmails(t *testing.T) {
	tests := map[string]struct {
		principal string
		email     string
		want      []string
	}{
		"same": {
			principal: "signup-e2e@verself.sh",
			email:     "signup-e2e@verself.sh",
			want:      []string{"signup-e2e@verself.sh"},
		},
		"plus alias": {
			principal: "signup@verself.sh",
			email:     "signup+agent@verself.sh",
			want:      []string{"signup@verself.sh", "signup+agent@verself.sh"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := testMailboxPrincipalEmails(tt.principal, tt.email); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("testMailboxPrincipalEmails() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTestMailboxPrincipalHasEmails(t *testing.T) {
	principal := stalwartPrincipal{Emails: []string{"signup@verself.sh", "signup+agent@verself.sh"}}
	if !testMailboxPrincipalHasEmails(principal, []string{"signup@verself.sh"}) {
		t.Fatalf("expected base address to be present")
	}
	if !testMailboxPrincipalHasEmails(principal, []string{"signup@verself.sh", "signup+agent@verself.sh"}) {
		t.Fatalf("expected plus alias to be present")
	}
	if testMailboxPrincipalHasEmails(principal, []string{"signup+other@verself.sh"}) {
		t.Fatalf("unexpected plus alias reported present")
	}
}

func TestTestMailboxCleanupPrincipals(t *testing.T) {
	got := testMailboxCleanupPrincipals([]string{
		"signup+agent@verself.sh",
		"signup+other@verself.sh",
		"signup-e2e@verself.sh",
	})
	want := []string{
		"signup+agent@verself.sh",
		"signup+other@verself.sh",
		"signup-e2e@verself.sh",
		"signup@verself.sh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("testMailboxCleanupPrincipals() = %#v, want %#v", got, want)
	}
}
