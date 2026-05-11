package iam

import (
	"context"
	"errors"

	auth "github.com/verself/service-runtime/auth"
)

var (
	ErrAuthorizerUnavailable = errors.New("iam authorizer unavailable")
	ErrInvalidIdentity       = errors.New("invalid authorization identity")
)

type OperationAuthorizer interface {
	AuthorizeOperation(ctx context.Context, identity *auth.Identity, policy OperationPolicy) (AuthorizationDecision, error)
}

type ResourceAuthorizer interface {
	OperationAuthorizer
	AuthorizeResource(ctx context.Context, identity *auth.Identity, request ResourceAuthorizationRequest) (ResourceAuthorizationDecision, error)
	EnsureResourceParent(ctx context.Context, request ResourceParentEdgeRequest) (ResourceParentEdgeDecision, error)
}

type AuthorizationDecision struct {
	Allowed     bool
	OrgID       string
	SubjectType string
	SubjectID   string
	Permission  Permission
	Resource    ResourceKind
	Action      Action
	OrgScope    OrgScope
	Permissions []Permission
	ZedToken    string
}

type ResourceRef struct {
	Type string
	ID   string
}

type ResourceAuthorizationRequest struct {
	OrgID               string
	OperationPermission Permission
	Resource            ResourceRef
	ResourcePermission  string
	MinZedToken         string
}

type ResourceAuthorizationDecision struct {
	Allowed             bool
	OrgID               string
	SubjectType         string
	SubjectID           string
	OperationPermission Permission
	Resource            ResourceRef
	ResourcePermission  string
	ZedToken            string
}

type ResourceParentEdgeRequest struct {
	OrgID     string
	Resource  ResourceRef
	Relation  string
	Parent    ResourceRef
	Operation string
}

type ResourceParentEdgeDecision struct {
	Resource  ResourceRef
	Relation  string
	Parent    ResourceRef
	ZedToken  string
	Operation string
}
