package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
)

func RegisterRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service, installationID string) {
	runtime := publicRuntime{service: svc, authz: authzSvc}
	handlers := publicHandlers{
		service:        svc,
		authz:          authzSvc,
		installationID: installationID,
	}
	registerPublicOperation(api, runtime, contractapi.ListOrganizations, handlers.ListOrganizations, "List organizations")
	registerPublicOperation(api, runtime, contractapi.CreateOrganization, handlers.CreateOrganization, "Create organization")
	registerPublicOperation(api, runtime, contractapi.GetOrganization, handlers.GetOrganization, "Get organization")
	registerPublicOperation(api, runtime, contractapi.UpdateOrganization, handlers.UpdateOrganization, "Update organization")
	registerPublicOperation(api, runtime, contractapi.ListMembers, handlers.ListMembers, "List members")
	registerPublicOperation(api, runtime, contractapi.GetMember, handlers.GetMember, "Get member")
	registerPublicOperation(api, runtime, contractapi.InviteMember, handlers.InviteMember, "Invite member")
	registerPublicOperation(api, runtime, contractapi.GetIamPolicy, handlers.GetIamPolicy, "Get IAM policy")
	registerPublicOperation(api, runtime, contractapi.SetIamPolicy, handlers.SetIamPolicy, "Set IAM policy")
	registerPublicOperation(api, runtime, contractapi.TestIamPermissions, handlers.TestIamPermissions, "Test IAM permissions")
}

func registerPublicOperation[Input any, Output any](
	api huma.API,
	runtime publicRuntime,
	operation contractapi.Operation[Input, Output],
	handler contractapi.Handler[Input, Output],
	summary string,
) {
	desc := operation.Descriptor
	op := runtime.PrepareOperation(desc, huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
	})
	huma.Register(api, op, func(ctx context.Context, input *Input) (*Output, error) {
		identity, err := runtime.BeforeOperation(ctx, desc, input)
		if err != nil {
			runtime.AfterOperation(ctx, desc, identity, input, nil, err)
			return nil, err
		}
		output, err := handler(ctx, input)
		runtime.AfterOperation(ctx, desc, identity, input, output, err)
		return output, err
	})
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	return identity, nil
}

func principalFromContext(ctx context.Context) (identity.Principal, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	return principalFromAuthIdentity(ctx, authIdentity)
}

func principalFromAuthIdentity(ctx context.Context, authIdentity *auth.Identity) (identity.Principal, error) {
	if authIdentity == nil {
		return identity.Principal{}, unauthorized(ctx)
	}
	return principalForAuthIdentityOrg(ctx, authIdentity, authIdentity.OrgID)
}

func principalForAuthIdentityOrg(ctx context.Context, authIdentity *auth.Identity, orgID string) (identity.Principal, error) {
	if authIdentity == nil {
		return identity.Principal{}, unauthorized(ctx)
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return identity.Principal{}, badRequest(ctx, "invalid-token-org", "token org_id is required", nil)
	}
	subject := authIdentity.Subject
	subjectKind := identity.AuthorizationSubjectKindUser
	if serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		subject = strings.TrimSpace(serviceAccountID)
		subjectKind = identity.AuthorizationSubjectKindServiceAccount
	}
	return identity.Principal{
		Subject:     subject,
		SubjectKind: subjectKind,
		OrgID:       orgID,
		Email:       authIdentity.Email,
	}, nil
}

func authzSubjectFromIdentity(authIdentity *auth.Identity) identity.AuthorizationSubject {
	if authIdentity == nil {
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindUser}
	}
	if credentialID, _ := authIdentity.Raw["verself:credential_id"].(string); strings.TrimSpace(credentialID) != "" {
		serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string)
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindServiceAccount, ID: strings.TrimSpace(serviceAccountID)}
	}
	if serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindServiceAccount, ID: strings.TrimSpace(serviceAccountID)}
	}
	return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindUser, ID: authIdentity.Subject}
}
