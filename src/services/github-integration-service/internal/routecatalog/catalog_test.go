package routecatalog

import "testing"

func TestGeneratedCatalogsValidate(t *testing.T) {
	if got := Public.Service.Name; got != "github-integration-service" {
		t.Fatalf("Public.Service.Name = %q, want github-integration-service", got)
	}
	if len(Public.Operations) != 14 {
		t.Fatalf("len(Public.Operations) = %d, want 14", len(Public.Operations))
	}
	for _, operationID := range []string{
		"start-github-app-setup",
		"get-github-setup-session",
		"complete-github-app-setup",
		"start-github-user-authorization",
		"complete-github-user-authorization",
		"list-github-installations",
		"get-github-installation",
		"sync-github-installation",
		"disconnect-github-installation",
		"list-github-repositories",
		"get-github-repository",
		"enable-github-repository",
		"disable-github-repository",
		"receive-github-webhook",
	} {
		if _, ok := Public.Operation(operationID); !ok {
			t.Fatalf("missing %s", operationID)
		}
	}
	if len(Internal.Operations) != 0 {
		t.Fatalf("len(Internal.Operations) = %d, want 0", len(Internal.Operations))
	}
}
