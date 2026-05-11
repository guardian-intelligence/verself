package iamclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

var ErrAuthorizerUnavailable = runtimeiam.ErrAuthorizerUnavailable
var ErrInvalidIdentity = runtimeiam.ErrInvalidIdentity

type OperationAuthorizer = runtimeiam.OperationAuthorizer

type Authorizer struct {
	Client *ClientWithResponses
}

type AuthorizationDecision = runtimeiam.AuthorizationDecision

type AuthorizationError struct {
	StatusCode int
	Body       string
}

func (e AuthorizationError) Error() string {
	status := http.StatusText(e.StatusCode)
	if status == "" {
		status = "unexpected status"
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("iam authorization failed with status %d %s", e.StatusCode, status)
	}
	return fmt.Sprintf("iam authorization failed with status %d %s: %s", e.StatusCode, status, body)
}

func NewAuthorizer(client *ClientWithResponses) Authorizer {
	return Authorizer{Client: client}
}

func (a Authorizer) AuthorizeOperation(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) (AuthorizationDecision, error) {
	if a.Client == nil {
		return AuthorizationDecision{}, ErrAuthorizerUnavailable
	}
	permission := strings.TrimSpace(string(policy.Permission))
	if permission == "" {
		return AuthorizationDecision{}, fmt.Errorf("%w: permission is required", ErrInvalidIdentity)
	}
	subject, err := AuthorizationSubjectForIdentity(identity)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	orgID := ""
	if identity != nil {
		orgID = strings.TrimSpace(identity.OrgID)
	}
	if orgID == "" {
		return AuthorizationDecision{}, fmt.Errorf("%w: org_id is required", ErrInvalidIdentity)
	}
	permissions := []string{permission}
	resp, err := a.Client.AuthorizeOperationWithResponse(ctx, AuthorizeOperationJSONRequestBody{
		OrgId:       orgID,
		Subject:     subject,
		Permissions: &permissions,
	})
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("%w: %v", ErrAuthorizerUnavailable, err)
	}
	if resp.JSON200 == nil {
		return AuthorizationDecision{}, AuthorizationError{StatusCode: resp.StatusCode(), Body: string(resp.Body)}
	}
	zedToken := ""
	if resp.JSON200.ZedToken != nil {
		zedToken = strings.TrimSpace(*resp.JSON200.ZedToken)
	}
	decision := AuthorizationDecision{
		OrgID:       strings.TrimSpace(resp.JSON200.OrgId),
		SubjectType: strings.TrimSpace(string(resp.JSON200.Subject.Type)),
		SubjectID:   strings.TrimSpace(resp.JSON200.Subject.Id),
		Permission:  policy.Permission,
		Resource:    policy.Resource,
		Action:      policy.Action,
		OrgScope:    policy.OrgScope,
		Permissions: permissionsFromResponse(resp.JSON200.Permissions),
		ZedToken:    zedToken,
	}
	for _, allowed := range decision.Permissions {
		if strings.TrimSpace(string(allowed)) == permission {
			decision.Allowed = true
			break
		}
	}
	return decision, nil
}

func AuthorizationSubjectForIdentity(identity *auth.Identity) (IAMAuthorizationSubject, error) {
	if identity == nil {
		return IAMAuthorizationSubject{}, fmt.Errorf("%w: identity is required", ErrInvalidIdentity)
	}
	if credentialID := claimString(identity.Raw, "verself:credential_id"); credentialID != "" {
		serviceAccountID := claimString(identity.Raw, "verself:service_account_id")
		if serviceAccountID == "" {
			return IAMAuthorizationSubject{}, fmt.Errorf("%w: service account credential missing verself:service_account_id", ErrInvalidIdentity)
		}
		return IAMAuthorizationSubject{Type: ServiceAccount, Id: serviceAccountID}, nil
	}
	subject := strings.TrimSpace(identity.Subject)
	if subject == "" {
		return IAMAuthorizationSubject{}, fmt.Errorf("%w: subject is required", ErrInvalidIdentity)
	}
	return IAMAuthorizationSubject{Type: User, Id: subject}, nil
}

func permissionsFromResponse(permissions *[]string) []runtimeiam.Permission {
	if permissions == nil {
		return nil
	}
	out := make([]runtimeiam.Permission, 0, len(*permissions))
	for _, permission := range *permissions {
		if permission = strings.TrimSpace(permission); permission != "" {
			out = append(out, runtimeiam.Permission(permission))
		}
	}
	return out
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}
