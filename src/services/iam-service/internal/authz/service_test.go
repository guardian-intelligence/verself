package authz

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/spicedb"
)

func TestCheckResourcePermissionUsesResourceGraph(t *testing.T) {
	subject := identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindServiceAccount, ID: "svc_123"}
	backend := &fakeBackend{
		checks: map[string]bool{
			checkKey(spicedb.ResourceRef{Type: resourceTypeAnalyticsDataset, ID: "dataset_1"}, "read", subjectRef(ServiceAccountSubject("svc_123"))): true,
		},
	}
	decision, err := New(backend).CheckResourcePermission(context.Background(), "org_1", subject, ResourceRef{Type: resourceTypeAnalyticsDataset, ID: "dataset_1"}, "read", identity.PermissionAnalyticsDatasetRead, "")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatalf("Allowed = false, want true")
	}
	if got := len(backend.checkCalls); got != 1 {
		t.Fatalf("backend checks = %d, want resource check only", got)
	}
}

func TestCheckResourcePermissionRejectsUnknownResourcePermission(t *testing.T) {
	subject := identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindUser, ID: "user_123"}
	_, err := New(&fakeBackend{}).CheckResourcePermission(context.Background(), "org_1", subject, ResourceRef{Type: resourceTypeAnalyticsDataset, ID: "dataset_1"}, "delete", identity.PermissionAnalyticsDatasetRead, "")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestWriteResourceParentEdgeAllowsOnlyModeledEdges(t *testing.T) {
	backend := &fakeBackend{}
	edge, err := New(backend).WriteResourceParentEdge(
		context.Background(),
		"org_1",
		ResourceRef{Type: resourceTypeAnalyticsDataset, ID: "dataset_1"},
		relationParentProject,
		ResourceRef{Type: resourceTypeProject, ID: "project_1"},
		"test.analytics_dataset.parent",
	)
	if err != nil {
		t.Fatal(err)
	}
	if edge.ZedToken == "" {
		t.Fatalf("ZedToken is empty")
	}
	want := []spicedb.Relationship{{
		Resource: spicedb.ResourceRef{Type: resourceTypeAnalyticsDataset, ID: "dataset_1"},
		Relation: relationParentProject,
		Subject:  spicedb.SubjectRef{Type: resourceTypeProject, ID: "project_1"},
	}}
	if !reflect.DeepEqual(backend.desired, want) {
		t.Fatalf("desired relationship = %#v, want %#v", backend.desired, want)
	}
	_, err = New(backend).WriteResourceParentEdge(
		context.Background(),
		"org_1",
		ResourceRef{Type: resourceTypeAPIActivity, ID: "org_2"},
		relationParentOrg,
		ResourceRef{Type: resourceTypeOrg, ID: "org_1"},
		"test.api_activity.parent",
	)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

type fakeBackend struct {
	checks     map[string]bool
	checkCalls []string
	desired    []spicedb.Relationship
}

func (b *fakeBackend) Check(_ context.Context, resource spicedb.ResourceRef, permission string, subject spicedb.SubjectRef, _ string) (bool, string, error) {
	key := checkKey(resource, permission, subject)
	b.checkCalls = append(b.checkCalls, key)
	return b.checks[key], "zed-check", nil
}

func (b *fakeBackend) LookupResources(_ context.Context, _ string, _ string, _ spicedb.SubjectRef, _ uint32, _ string) ([]string, string, error) {
	return nil, "zed-lookup", nil
}

func (b *fakeBackend) ReadResourceRelationships(_ context.Context, _ spicedb.ResourceRef, _ map[string]struct{}) ([]spicedb.Relationship, string, error) {
	return nil, "zed-read", nil
}

func (b *fakeBackend) ReplaceResourceRelationships(_ context.Context, _ []spicedb.Relationship, desired []spicedb.Relationship, _ map[string]any) (string, error) {
	b.desired = append([]spicedb.Relationship(nil), desired...)
	return "zed-write", nil
}

func checkKey(resource spicedb.ResourceRef, permission string, subject spicedb.SubjectRef) string {
	return resource.Type + "\x00" + resource.ID + "\x00" + permission + "\x00" + subject.Type + "\x00" + subject.ID + "\x00" + subject.Relation
}
