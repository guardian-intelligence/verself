package api

import "testing"

func TestRuntimeSecretAllowedWithExactNameAndPrefix(t *testing.T) {
	policy := runtimeSecretPolicy{
		secretNames: map[string]struct{}{
			"billing-service.stripe.secret_key": {},
		},
		secretNamePrefixes: []string{"sandbox-rental-service.runner-bootstrap."},
	}

	for _, secretName := range []string{
		"billing-service.stripe.secret_key",
		"sandbox-rental-service.runner-bootstrap.github.00000000-0000-0000-0000-000000000000.11111111-1111-1111-1111-111111111111",
	} {
		if !runtimeSecretAllowed(policy, secretName) {
			t.Fatalf("expected runtime secret %q to be allowed", secretName)
		}
	}

	if runtimeSecretAllowed(policy, "sandbox-rental-service.github.private_key") {
		t.Fatal("unexpectedly allowed a runtime secret outside the configured selectors")
	}
}

func TestNormalizeRuntimeSecretSelectorsRejectsUnsafeSelectors(t *testing.T) {
	if _, _, err := normalizeRuntimeSecretSelectors(nil, nil); err == nil {
		t.Fatal("expected empty selector set to fail")
	}
	if _, _, err := normalizeRuntimeSecretSelectors([]string{"safe/name"}, nil); err == nil {
		t.Fatal("expected runtime secret name with path separator to fail")
	}
	if _, _, err := normalizeRuntimeSecretSelectors(nil, []string{"safe\tprefix"}); err == nil {
		t.Fatal("expected runtime secret prefix with control character to fail")
	}
}
