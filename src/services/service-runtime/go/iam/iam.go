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
	AuthorizeOperation(ctx context.Context, identity *auth.Identity, permission string) (AuthorizationDecision, error)
}

type AuthorizationDecision struct {
	Allowed     bool
	OrgID       string
	SubjectType string
	SubjectID   string
	Permissions []string
	ZedToken    string
}
