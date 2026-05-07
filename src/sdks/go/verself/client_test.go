package verself

import "testing"

func TestServiceURLDerivesServiceSubdomainFromInstallationApex(t *testing.T) {
	iamURL, err := serviceURL("", "https://example.com/console?ignored=true", "iam")
	if err != nil {
		t.Fatal(err)
	}
	if iamURL != "https://iam.api.example.com" {
		t.Fatalf("iam URL = %q", iamURL)
	}
	projectsURL, err := serviceURL("", "https://example.com:8443", "projects")
	if err != nil {
		t.Fatal(err)
	}
	if projectsURL != "https://projects.api.example.com:8443" {
		t.Fatalf("projects URL = %q", projectsURL)
	}
}

func TestServiceURLUsesExplicitServiceOverride(t *testing.T) {
	got, err := serviceURL("https://projects.internal.example.com/base/", "https://example.com", "projects")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://projects.internal.example.com/base" {
		t.Fatalf("service URL = %q", got)
	}
}

func TestServiceURLRejectsServiceHostAsInstallationApex(t *testing.T) {
	if _, err := serviceURL("", "https://iam.api.example.com", "projects"); err == nil {
		t.Fatal("expected service host error")
	}
}
