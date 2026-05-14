package main

import "testing"

func TestPlatformRuntimeAuthAudienceSpecsUseProductAudience(t *testing.T) {
	credentialPaths := map[string]string{}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		if previous, ok := credentialPaths[spec.CredentialPath]; ok {
			t.Fatalf("duplicate runtime auth audience credential path %s for %s and %s", spec.CredentialPath, previous, spec.ComponentName)
		}
		credentialPaths[spec.CredentialPath] = spec.ComponentName
		if spec.ComponentName == "" || spec.CredentialPath == "" || spec.Group == "" {
			t.Fatalf("incomplete runtime auth audience spec: %#v", spec)
		}
	}
	if platformProductAPIProjectName != "verself-api" {
		t.Fatalf("product API project name = %q", platformProductAPIProjectName)
	}
}
