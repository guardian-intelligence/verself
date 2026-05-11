package dto

import "testing"

func TestResourceNameRoundTrip(t *testing.T) {
	name := ResourceNameProject("inst_5NZSEA08R8P3HN566DNH8D301M", "371564185181576922", "a9bd2d1c-f20f-4f24-ae9f-9d4f538c2f7b")
	parsed, err := ParseResourceName(name.String())
	if err != nil {
		t.Fatalf("parse resource name: %v", err)
	}
	if parsed.InstallationID != "inst_5NZSEA08R8P3HN566DNH8D301M" {
		t.Fatalf("unexpected installation id %q", parsed.InstallationID)
	}
	if got := parsed.Leaf(); got.Collection != "projects" || got.ID != "a9bd2d1c-f20f-4f24-ae9f-9d4f538c2f7b" {
		t.Fatalf("unexpected leaf %#v", got)
	}
	if got, ok := parsed.IDForCollection("orgs"); !ok || got != "371564185181576922" {
		t.Fatalf("unexpected org collection lookup %q %v", got, ok)
	}
	if parsed.String() != name.String() {
		t.Fatalf("round trip mismatch: %s != %s", parsed.String(), name.String())
	}
}

func TestParseResourceNameRejectsMalformedValues(t *testing.T) {
	tests := []string{
		"",
		"urn:other:inst_1:orgs/1",
		"urn:verself:inst_1",
		"urn:verself:inst_1:orgs",
		"urn:verself:inst_1:Orgs/1",
		"urn:verself:inst_1:orgs/has/slash",
		"urn:verself:inst 1:orgs/1",
	}
	for _, test := range tests {
		if _, err := ParseResourceName(test); err == nil {
			t.Fatalf("expected %q to be rejected", test)
		}
	}
}
