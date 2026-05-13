package api

import (
	"context"
	"sort"
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
	registerPublicOperation(api, runtime, contractapi.GetOrganization, handlers.GetOrganization, "Get organization")
	registerPublicOperation(api, runtime, contractapi.UpdateOrganization, handlers.UpdateOrganization, "Update organization")
	registerPublicOperation(api, runtime, contractapi.ListMembers, handlers.ListMembers, "List members")
	registerPublicOperation(api, runtime, contractapi.GetMember, handlers.GetMember, "Get member")
	registerPublicOperation(api, runtime, contractapi.UpdateMemberRole, handlers.UpdateMemberRole, "Update member role")
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
		Roles:       identityRolesForCurrentOrg(authIdentity),
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

func roleAssignmentOrgIDs(ctx context.Context, authIdentity *auth.Identity) ([]string, error) {
	if authIdentity == nil {
		return nil, unauthorized(ctx)
	}
	seen := map[string]struct{}{}
	orgIDs := make([]string, 0, len(authIdentity.RoleAssignments))
	for _, assignment := range authIdentity.RoleAssignments {
		orgID := strings.TrimSpace(assignment.OrganizationID)
		if orgID == "" {
			continue
		}
		if _, ok := seen[orgID]; ok {
			continue
		}
		seen[orgID] = struct{}{}
		orgIDs = append(orgIDs, orgID)
	}
	if len(orgIDs) == 0 {
		return nil, forbidden(ctx, "organization-role-assignment-required", "token does not carry any organization role assignments")
	}
	sort.Strings(orgIDs)
	return orgIDs, nil
}
