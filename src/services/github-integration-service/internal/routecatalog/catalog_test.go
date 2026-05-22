package routecatalog

import "testing"

func TestGeneratedCatalogsValidate(t *testing.T) {
	if got := Public.Service.Name; got != "github-integration-service" {
		t.Fatalf("Public.Service.Name = %q, want github-integration-service", got)
	}
	if len(Public.Operations) != 1 {
		t.Fatalf("len(Public.Operations) = %d, want 1", len(Public.Operations))
	}
	if _, ok := Public.Operation("receive-github-webhook"); !ok {
		t.Fatalf("missing receive-github-webhook")
	}
	if len(Internal.Operations) != 0 {
		t.Fatalf("len(Internal.Operations) = %d, want 0", len(Internal.Operations))
	}
}
