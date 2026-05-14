package main

import (
	"strings"
	"testing"
)

func TestPlatformRuntimeAuthAudienceSpecsKeepWebAudienceWithRuntimeAudience(t *testing.T) {
	credentialPaths := map[string]string{}
	runtimeProjects := map[string]string{}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		if previous, ok := credentialPaths[spec.CredentialPath]; ok {
			t.Fatalf("duplicate runtime auth audience credential path %s for %s and %s", spec.CredentialPath, previous, spec.ComponentName)
		}
		credentialPaths[spec.CredentialPath] = spec.ComponentName
		if !strings.HasPrefix(spec.ComponentName, "verself-web.") {
			runtimeProjects[spec.ComponentName] = spec.ProjectName
		}
	}
	for _, spec := range platformRuntimeAuthAudienceSpecs() {
		if !spec.BrowserLoginAudience {
			continue
		}
		componentName := strings.TrimPrefix(spec.ComponentName, "verself-web.")
		projectName, ok := runtimeProjects[componentName]
		if !ok {
			t.Fatalf("browser auth audience %s has no runtime audience spec", spec.ComponentName)
		}
		if spec.ProjectName != projectName {
			t.Fatalf("browser auth audience %s project = %q, runtime project = %q", spec.ComponentName, spec.ProjectName, projectName)
		}
		if spec.Group != "verself-web" {
			t.Fatalf("browser auth audience %s credential group = %q", spec.ComponentName, spec.Group)
		}
	}
}
