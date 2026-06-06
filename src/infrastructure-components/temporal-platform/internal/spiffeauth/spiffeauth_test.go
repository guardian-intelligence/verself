package spiffeauth

import (
	"testing"

	"go.temporal.io/server/common/authorization"
)

func TestFromDocumentBuildsSystemAndNamespaceRoles(t *testing.T) {
	cfg, err := FromDocument(Document{
		SystemAdminIDs: []string{"spiffe://gamma.verself.sh/svc/temporal-server"},
		NamespaceRoles: []NamespaceRole{
			{
				SPIFFEID:  "spiffe://gamma.verself.sh/svc/billing-service",
				Namespace: "billing-service",
				Role:      "admin",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.systemRoles["spiffe://gamma.verself.sh/svc/temporal-server"]; got != authorization.RoleAdmin {
		t.Fatalf("system role = %v, want admin", got)
	}
	if got := cfg.namespaceRoles["spiffe://gamma.verself.sh/svc/billing-service"]["billing-service"]; got != authorization.RoleAdmin {
		t.Fatalf("namespace role = %v, want admin", got)
	}
}

func TestFromDocumentRejectsUnknownNamespaceRole(t *testing.T) {
	_, err := FromDocument(Document{
		NamespaceRoles: []NamespaceRole{
			{
				SPIFFEID:  "spiffe://gamma.verself.sh/svc/billing-service",
				Namespace: "billing-service",
				Role:      "owner",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for unsupported namespace role")
	}
}
