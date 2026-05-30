package main

import (
	"strings"
	"testing"
)

func TestForgejoConfigUsesSiteDomainAndLoopbackListener(t *testing.T) {
	body := forgejoConfig(config{
		SiteDomain:    "gamma.verself.sh",
		ForgejoDomain: "git.gamma.verself.sh",
		HTTPAddr:      "127.0.0.1",
		HTTPPort:      "3000",
	})
	for _, want := range []string{
		"DOMAIN = git.gamma.verself.sh",
		"ROOT_URL = https://git.gamma.verself.sh/",
		"HTTP_ADDR = 127.0.0.1",
		"HTTP_PORT = 3000",
		"DISABLE_SSH = true",
		"ENABLE_BASIC_AUTHENTICATION = false",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("config missing %q:\n%s", want, body)
		}
	}
}

func TestWithEnvReplacesExistingKeys(t *testing.T) {
	env := withEnv([]string{"PATH=/host/bin", "KEEP=value", "PATH=/second"}, map[string]string{"PATH": "/task/bin"})
	if strings.Join(env, "\n") != "PATH=/task/bin\nKEEP=value" {
		t.Fatalf("unexpected env: %#v", env)
	}
}
