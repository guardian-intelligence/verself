package iamclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
type ResourceAuthorizationDecision = runtimeiam.ResourceAuthorizationDecision
type ResourceParentEdgeDecision = runtimeiam.ResourceParentEdgeDecision

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

func (a Authorizer) AuthorizeResource(ctx context.Context, identity *auth.Identity, request runtimeiam.ResourceAuthorizationRequest) (ResourceAuthorizationDecision, error) {
	client, err := a.rawClient()
	if err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	operationPermission := strings.TrimSpace(string(request.OperationPermission))
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
	wireRequest := authorizeResourceWireRequest{
		OrgID:               orgID,
		Subject:             authorizationSubjectWire{Type: strings.TrimSpace(string(subject.Type)), ID: strings.TrimSpace(subject.Id)},
		OperationPermission: operationPermission,
		Resource:            resourceRefWire{Type: strings.TrimSpace(request.Resource.Type), ID: strings.TrimSpace(request.Resource.ID)},
		ResourcePermission:  resourcePermission,
		MinZedToken:         strings.TrimSpace(request.MinZedToken),
	}
	var wireResponse authorizeResourceWireResponse
	if err := postInternalJSON(ctx, client, "/internal/v1/authorization/resources/authorize", wireRequest, &wireResponse); err != nil {
		return ResourceAuthorizationDecision{}, err
	}
	return ResourceAuthorizationDecision{
		Allowed:             wireResponse.Allowed,
		OrgID:               strings.TrimSpace(wireResponse.OrgID),
		SubjectType:         strings.TrimSpace(wireResponse.Subject.Type),
		SubjectID:           strings.TrimSpace(wireResponse.Subject.ID),
		OperationPermission: runtimeiam.Permission(strings.TrimSpace(wireResponse.OperationPermission)),
		Resource:            runtimeiam.ResourceRef{Type: strings.TrimSpace(wireResponse.Resource.Type), ID: strings.TrimSpace(wireResponse.Resource.ID)},
		ResourcePermission:  strings.TrimSpace(wireResponse.ResourcePermission),
		ZedToken:            strings.TrimSpace(wireResponse.ZedToken),
	}, nil
}

func (a Authorizer) EnsureResourceParent(ctx context.Context, request runtimeiam.ResourceParentEdgeRequest) (ResourceParentEdgeDecision, error) {
	client, err := a.rawClient()
	if err != nil {
		return ResourceParentEdgeDecision{}, err
	}
	orgID := strings.TrimSpace(request.OrgID)
	if orgID == "" {
		return ResourceParentEdgeDecision{}, fmt.Errorf("%w: org_id is required", ErrInvalidIdentity)
	}
	wireRequest := writeResourceParentEdgeWireRequest{
		OrgID:     orgID,
		Resource:  resourceRefWire{Type: strings.TrimSpace(request.Resource.Type), ID: strings.TrimSpace(request.Resource.ID)},
		Relation:  strings.TrimSpace(request.Relation),
		Parent:    resourceRefWire{Type: strings.TrimSpace(request.Parent.Type), ID: strings.TrimSpace(request.Parent.ID)},
		Operation: strings.TrimSpace(request.Operation),
	}
	var wireResponse writeResourceParentEdgeWireResponse
	if err := postInternalJSON(ctx, client, "/internal/v1/authorization/resources/parent-edge", wireRequest, &wireResponse); err != nil {
		return ResourceParentEdgeDecision{}, err
	}
	return ResourceParentEdgeDecision{
		Resource:  runtimeiam.ResourceRef{Type: strings.TrimSpace(wireResponse.Resource.Type), ID: strings.TrimSpace(wireResponse.Resource.ID)},
		Relation:  strings.TrimSpace(wireResponse.Relation),
		Parent:    runtimeiam.ResourceRef{Type: strings.TrimSpace(wireResponse.Parent.Type), ID: strings.TrimSpace(wireResponse.Parent.ID)},
		ZedToken:  strings.TrimSpace(wireResponse.ZedToken),
		Operation: strings.TrimSpace(wireResponse.Operation),
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
		return IAMAuthorizationSubject{Type: ServiceAccount, Id: serviceAccountID}, nil
	}
	subject := strings.TrimSpace(identity.Subject)
	if subject == "" {
		return IAMAuthorizationSubject{}, fmt.Errorf("%w: subject is required", ErrInvalidIdentity)
	}
	return IAMAuthorizationSubject{Type: User, Id: subject}, nil
}

func (a Authorizer) rawClient() (*Client, error) {
	if a.Client == nil || a.Client.ClientInterface == nil {
		return nil, ErrAuthorizerUnavailable
	}
	client, ok := a.Client.ClientInterface.(*Client)
	if !ok || client == nil || client.Client == nil {
		return nil, ErrAuthorizerUnavailable
	}
	return client, nil
}

type authorizationSubjectWire struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type resourceRefWire struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type authorizeResourceWireRequest struct {
	OrgID               string                   `json:"org_id"`
	Subject             authorizationSubjectWire `json:"subject"`
	OperationPermission string                   `json:"operation_permission,omitempty"`
	Resource            resourceRefWire          `json:"resource"`
	ResourcePermission  string                   `json:"resource_permission"`
	MinZedToken         string                   `json:"min_zed_token,omitempty"`
}

type authorizeResourceWireResponse struct {
	OrgID               string                   `json:"org_id"`
	Subject             authorizationSubjectWire `json:"subject"`
	OperationPermission string                   `json:"operation_permission,omitempty"`
	Resource            resourceRefWire          `json:"resource"`
	ResourcePermission  string                   `json:"resource_permission"`
	Allowed             bool                     `json:"allowed"`
	ZedToken            string                   `json:"zed_token,omitempty"`
}

type writeResourceParentEdgeWireRequest struct {
	OrgID     string          `json:"org_id"`
	Resource  resourceRefWire `json:"resource"`
	Relation  string          `json:"relation"`
	Parent    resourceRefWire `json:"parent"`
	Operation string          `json:"operation,omitempty"`
}

type writeResourceParentEdgeWireResponse struct {
	Resource  resourceRefWire `json:"resource"`
	Relation  string          `json:"relation"`
	Parent    resourceRefWire `json:"parent"`
	ZedToken  string          `json:"zed_token,omitempty"`
	Operation string          `json:"operation,omitempty"`
}

func postInternalJSON(ctx context.Context, client *Client, path string, request any, response any) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("%w: marshal iam internal request: %v", ErrAuthorizerUnavailable, err)
	}
	endpoint := strings.TrimRight(client.Server, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: build iam internal request: %v", ErrAuthorizerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if err := client.applyEditors(ctx, req, nil); err != nil {
		return fmt.Errorf("%w: edit iam internal request: %v", ErrAuthorizerUnavailable, err)
	}
	resp, err := client.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAuthorizerUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read iam internal response: %v", ErrAuthorizerUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AuthorizationError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("%w: decode iam internal response: %v", ErrAuthorizerUnavailable, err)
	}
	return nil
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
