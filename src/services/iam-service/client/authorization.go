package iamclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

var (
	ErrAuthorizerUnavailable = runtimeiam.ErrAuthorizerUnavailable
	ErrInvalidIdentity       = runtimeiam.ErrInvalidIdentity
)

type OperationAuthorizer = runtimeiam.OperationAuthorizer

type Authorizer struct {
	Client *Client
}

type (
	AuthorizationDecision         = runtimeiam.AuthorizationDecision
	ResourceAuthorizationDecision = runtimeiam.ResourceAuthorizationDecision
	ResourceParentEdgeDecision    = runtimeiam.ResourceParentEdgeDecision
)

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

func NewAuthorizer(client *Client) Authorizer {
	return Authorizer{Client: client}
}

func (a Authorizer) AuthorizeOperation(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) (AuthorizationDecision, error) {
	client, err := a.client()
	if err != nil {
		return AuthorizationDecision{}, err
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
	resp, err := client.AuthorizeOperation(ctx, AuthorizeOperationRequest{
		Body: AuthorizeOperationInputBody{
			OrgID:       OrgId(orgID),
			Subject:     subject,
			Permissions: Permissions{PermissionName(permission)},
		},
	})
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("%w: %v", ErrAuthorizerUnavailable, err)
	}
	if resp.Result == nil {
		return AuthorizationDecision{}, AuthorizationError{StatusCode: resp.StatusCode, Body: string(resp.Body)}
	}
	decision := AuthorizationDecision{
		OrgID:       strings.TrimSpace(string(resp.Result.OrgID)),
		SubjectType: strings.TrimSpace(string(resp.Result.Subject.Type)),
		SubjectID:   strings.TrimSpace(string(resp.Result.Subject.ID)),
		Permission:  policy.Permission,
		Resource:    policy.Resource,
		Action:      policy.Action,
		OrgScope:    policy.OrgScope,
		Permissions: permissionsFromResponse(resp.Result.Permissions),
		ZedToken:    zedTokenValue(resp.Result.ZedToken),
	}
	for _, allowed := range decision.Permissions {
		if strings.TrimSpace(string(allowed)) == permission {
			decision.Allowed = true
			break
		}
	}
	return decision, nil
}

func (a Authorizer) AuthorizeResource(ctx context.Context, identity *auth.Identity, request runtimeiam.ResourceAuthorizationRequest) (ResourceAuthorizationDecision, error) {
	client, err := a.client()
	if err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	resourcePermission := strings.TrimSpace(request.ResourcePermission)
	if resourcePermission == "" {
		return ResourceAuthorizationDecision{}, fmt.Errorf("%w: resource permission is required", ErrInvalidIdentity)
	}
	subject, err := AuthorizationSubjectForIdentity(identity)
	if err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	orgID := strings.TrimSpace(request.OrgID)
	if orgID == "" && identity != nil {
		orgID = strings.TrimSpace(identity.OrgID)
	}
	if orgID == "" {
		return ResourceAuthorizationDecision{}, fmt.Errorf("%w: org_id is required", ErrInvalidIdentity)
	}
	resp, err := client.AuthorizeResource(ctx, AuthorizeResourceRequest{
		Body: AuthorizeResourceInputBody{
			OrgID:               OrgId(orgID),
			Subject:             subject,
			OperationPermission: permissionPtr(string(request.OperationPermission)),
			Resource:            resourceRef(request.Resource),
			ResourcePermission:  ResourcePermissionName(resourcePermission),
			MinZedToken:         zedTokenPtr(request.MinZedToken),
		},
	})
	if err != nil {
		return ResourceAuthorizationDecision{}, fmt.Errorf("%w: %v", ErrAuthorizerUnavailable, err)
	}
	if resp.Result == nil {
		return ResourceAuthorizationDecision{}, AuthorizationError{StatusCode: resp.StatusCode, Body: string(resp.Body)}
	}
	return ResourceAuthorizationDecision{
		Allowed:             resp.Result.Allowed,
		OrgID:               strings.TrimSpace(string(resp.Result.OrgID)),
		SubjectType:         strings.TrimSpace(string(resp.Result.Subject.Type)),
		SubjectID:           strings.TrimSpace(string(resp.Result.Subject.ID)),
		OperationPermission: runtimeiam.Permission(permissionValue(resp.Result.OperationPermission)),
		Resource:            runtimeiam.ResourceRef{Type: strings.TrimSpace(string(resp.Result.Resource.Type)), ID: strings.TrimSpace(string(resp.Result.Resource.ID))},
		ResourcePermission:  strings.TrimSpace(string(resp.Result.ResourcePermission)),
		ZedToken:            zedTokenValue(resp.Result.ZedToken),
	}, nil
}

func (a Authorizer) EnsureResourceParent(ctx context.Context, request runtimeiam.ResourceParentEdgeRequest) (ResourceParentEdgeDecision, error) {
	client, err := a.client()
	if err != nil {
		return ResourceParentEdgeDecision{}, err
	}
	orgID := strings.TrimSpace(request.OrgID)
	if orgID == "" {
		return ResourceParentEdgeDecision{}, fmt.Errorf("%w: org_id is required", ErrInvalidIdentity)
	}
	resp, err := client.WriteResourceParentEdge(ctx, WriteResourceParentEdgeRequest{
		Body: WriteResourceParentEdgeInputBody{
			OrgID:     OrgId(orgID),
			Resource:  resourceRef(request.Resource),
			Relation:  AuthorizationRelation(strings.TrimSpace(request.Relation)),
			Parent:    resourceRef(request.Parent),
			Operation: permissionPtr(request.Operation),
		},
	})
	if err != nil {
		return ResourceParentEdgeDecision{}, fmt.Errorf("%w: %v", ErrAuthorizerUnavailable, err)
	}
	if resp.Result == nil {
		return ResourceParentEdgeDecision{}, AuthorizationError{StatusCode: resp.StatusCode, Body: string(resp.Body)}
	}
	return ResourceParentEdgeDecision{
		Resource:  runtimeiam.ResourceRef{Type: strings.TrimSpace(string(resp.Result.Resource.Type)), ID: strings.TrimSpace(string(resp.Result.Resource.ID))},
		Relation:  strings.TrimSpace(string(resp.Result.Relation)),
		Parent:    runtimeiam.ResourceRef{Type: strings.TrimSpace(string(resp.Result.Parent.Type)), ID: strings.TrimSpace(string(resp.Result.Parent.ID))},
		ZedToken:  zedTokenValue(resp.Result.ZedToken),
		Operation: permissionValue(resp.Result.Operation),
	}, nil
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
		return IAMAuthorizationSubject{Type: IAMAuthorizationSubjectTypeSERVICEACCOUNT, ID: SubjectId(serviceAccountID)}, nil
	}
	subject := strings.TrimSpace(identity.Subject)
	if subject == "" {
		return IAMAuthorizationSubject{}, fmt.Errorf("%w: subject is required", ErrInvalidIdentity)
	}
	return IAMAuthorizationSubject{Type: IAMAuthorizationSubjectTypeUSER, ID: SubjectId(subject)}, nil
}

func (a Authorizer) client() (*Client, error) {
	if a.Client == nil {
		return nil, ErrAuthorizerUnavailable
	}
	return a.Client, nil
}

func resourceRef(resource runtimeiam.ResourceRef) IAMResourceRef {
	return IAMResourceRef{
		Type: AuthorizationResourceType(strings.TrimSpace(resource.Type)),
		ID:   AuthorizationResourceId(strings.TrimSpace(resource.ID)),
	}
}

func permissionsFromResponse(permissions Permissions) []runtimeiam.Permission {
	out := make([]runtimeiam.Permission, 0, len(permissions))
	for _, permission := range permissions {
		if permission = PermissionName(strings.TrimSpace(string(permission))); permission != "" {
			out = append(out, runtimeiam.Permission(permission))
		}
	}
	return out
}

func permissionPtr(value string) *PermissionName {
	if value = strings.TrimSpace(value); value != "" {
		out := PermissionName(value)
		return &out
	}
	return nil
}

func permissionValue(value *PermissionName) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func zedTokenPtr(value string) *ZedToken {
	if value = strings.TrimSpace(value); value != "" {
		out := ZedToken(value)
		return &out
	}
	return nil
}

func zedTokenValue(value *ZedToken) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}
