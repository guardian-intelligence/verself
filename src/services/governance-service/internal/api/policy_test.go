package api

import (
	"context"
	"testing"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func TestEnforceOperationPolicyWritesAPIActivityParentEdgeWithPermissionName(t *testing.T) {
	authorizer := &recordingResourceAuthorizer{}
	policy := runtimeiam.OperationPolicy{
		Permission:     permissionAPIActivityRead,
		Resource:       "api_activity",
		Action:         runtimeiam.ActionList,
		OrgScope:       runtimeiam.OrgScopeTokenOrgID,
		RateLimitClass: "read",
		AuditEvent:     "governance.ocsf_api_activity.list",
	}
	ctx := auth.WithIdentity(context.Background(), &auth.Identity{
		Subject: "user_123",
		OrgID:   "371564185181576922",
	})

	if _, err := enforceOperationPolicy(ctx, authorizer, policy); err != nil {
		t.Fatalf("enforceOperationPolicy returned error: %v", err)
	}
	if authorizer.parentEdge.Operation != string(policy.Permission) {
		t.Fatalf("parent edge operation = %q, want %q", authorizer.parentEdge.Operation, policy.Permission)
	}
}

type recordingResourceAuthorizer struct {
	parentEdge runtimeiam.ResourceParentEdgeRequest
}

func (a *recordingResourceAuthorizer) AuthorizeOperation(context.Context, *auth.Identity, runtimeiam.OperationPolicy) (runtimeiam.AuthorizationDecision, error) {
	return runtimeiam.AuthorizationDecision{Allowed: true}, nil
}

func (a *recordingResourceAuthorizer) EnsureResourceParent(_ context.Context, request runtimeiam.ResourceParentEdgeRequest) (runtimeiam.ResourceParentEdgeDecision, error) {
	a.parentEdge = request
	return runtimeiam.ResourceParentEdgeDecision{
		Resource:  request.Resource,
		Relation:  request.Relation,
		Parent:    request.Parent,
		Operation: request.Operation,
	}, nil
}

func (a *recordingResourceAuthorizer) AuthorizeResource(_ context.Context, identity *auth.Identity, request runtimeiam.ResourceAuthorizationRequest) (runtimeiam.ResourceAuthorizationDecision, error) {
	return runtimeiam.ResourceAuthorizationDecision{
		Allowed:             true,
		OrgID:               identity.OrgID,
		SubjectID:           identity.Subject,
		OperationPermission: request.OperationPermission,
		Resource:            request.Resource,
		ResourcePermission:  request.ResourcePermission,
	}, nil
}
