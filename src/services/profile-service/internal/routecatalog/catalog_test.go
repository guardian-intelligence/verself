package routecatalog

import "testing"

func TestGeneratedCatalogsValidate(t *testing.T) {
	if got := Public.Service.Name; got != "profile-service" {
		t.Fatalf("Public.Service.Name = %q, want profile-service", got)
	}
	if len(Public.Operations) != 3 {
		t.Fatalf("len(Public.Operations) = %d, want 3", len(Public.Operations))
	}
	if _, ok := Public.Operation("patch-profile-identity"); !ok {
		t.Fatalf("missing patch-profile-identity")
	}
	if _, ok := Internal.Operation("profile-subject-erasure"); !ok {
		t.Fatalf("missing profile-subject-erasure")
	}
}
