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
