package api

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/forge-metal/auth-middleware"
)

type permission string

const (
	permissionRepoRead        permission = "sandbox:repo:read"
	permissionRepoWrite       permission = "sandbox:repo:write"
	permissionExecutionSubmit permission = "sandbox:execution:submit"
	permissionExecutionRead   permission = "sandbox:execution:read"
	permissionLogsRead        permission = "sandbox:logs:read"
	permissionBillingRead     permission = "billing:read"
	permissionBillingCheckout permission = "billing:checkout"

	roleSandboxOrgAdmin  = "sandbox_org_admin"
	roleSandboxOrgMember = "sandbox_org_member"
)

type operationPolicy struct {
	Permission     permission
	Resource       string
	Action         string
	OrgScope       string
	RateLimitClass string
	Idempotency    string
	AuditEvent     string
}

var publicAPIOperationPolicies = map[string]operationPolicy{
	"import-repo": {
		Permission:     permissionRepoWrite,
		Resource:       "repo",
		Action:         "import",
		OrgScope:       "token_org_id",
		RateLimitClass: "repo_mutation",
		Idempotency:    "provider_repo_id",
		AuditEvent:     "sandbox.repo.import",
	},
	"list-repos": {
		Permission:     permissionRepoRead,
		Resource:       "repo",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.repo.list",
	},
	"get-repo": {
		Permission:     permissionRepoRead,
		Resource:       "repo",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.repo.read",
	},
	"rescan-repo": {
		Permission:     permissionRepoWrite,
		Resource:       "repo",
		Action:         "rescan",
		OrgScope:       "token_org_id",
		RateLimitClass: "repo_mutation",
		AuditEvent:     "sandbox.repo.rescan",
	},
	"list-repo-generations": {
		Permission:     permissionRepoRead,
		Resource:       "repo_generation",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.repo_generation.list",
	},
	"refresh-repo": {
		Permission:     permissionRepoWrite,
		Resource:       "repo",
		Action:         "refresh",
		OrgScope:       "token_org_id",
		RateLimitClass: "repo_mutation",
		AuditEvent:     "sandbox.repo.refresh",
	},
	"submit-execution": {
		Permission:     permissionExecutionSubmit,
		Resource:       "execution",
		Action:         "submit",
		OrgScope:       "token_org_id",
		RateLimitClass: "execution_submit",
		Idempotency:    "request_body_idempotency_key",
		AuditEvent:     "sandbox.execution.submit",
	},
	"get-execution": {
		Permission:     permissionExecutionRead,
		Resource:       "execution",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution.read",
	},
	"get-execution-logs": {
		Permission:     permissionLogsRead,
		Resource:       "execution_logs",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "logs_read",
		AuditEvent:     "sandbox.execution.logs.read",
	},
	"get-billing-balance": {
		Permission:     permissionBillingRead,
		Resource:       "billing_balance",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.balance.read",
	},
	"list-billing-subscriptions": {
		Permission:     permissionBillingRead,
		Resource:       "billing_subscription",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.subscription.list",
	},
	"list-billing-grants": {
		Permission:     permissionBillingRead,
		Resource:       "billing_grant",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.grant.list",
	},
	"create-billing-checkout": {
		Permission:     permissionBillingCheckout,
		Resource:       "billing_checkout",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		AuditEvent:     "billing.checkout.create",
	},
	"create-billing-subscription": {
		Permission:     permissionBillingCheckout,
		Resource:       "billing_subscription_checkout",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		AuditEvent:     "billing.subscription_checkout.create",
	},
}

var rolePermissionBundles = map[string][]permission{
	roleSandboxOrgAdmin: {
		permissionRepoRead,
		permissionRepoWrite,
		permissionExecutionSubmit,
		permissionExecutionRead,
		permissionLogsRead,
		permissionBillingRead,
		permissionBillingCheckout,
	},
	roleSandboxOrgMember: {
		permissionRepoRead,
		permissionRepoWrite,
		permissionExecutionSubmit,
		permissionExecutionRead,
		permissionLogsRead,
	},
}

func registerSecured[I, O any](api huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	policy, ok := publicAPIOperationPolicies[op.OperationID]
	if !ok {
		panic("missing IAM policy for operation: " + op.OperationID)
	}
	op = withOperationPolicy(op, policy)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		if err := authorizeOperation(ctx, policy); err != nil {
			return nil, err
		}
		return handler(ctx, input)
	})
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" {
		panic("empty IAM permission for operation: " + op.OperationID)
	}
	if policy.OrgScope == "" {
		panic("empty IAM org scope for operation: " + op.OperationID)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	iam := map[string]any{
		"permission":       string(policy.Permission),
		"resource":         policy.Resource,
		"action":           policy.Action,
		"org_scope":        policy.OrgScope,
		"rate_limit_class": policy.RateLimitClass,
		"audit_event":      policy.AuditEvent,
	}
	if policy.Idempotency != "" {
		iam["idempotency"] = policy.Idempotency
	}
	op.Extensions["x-forge-metal-iam"] = iam
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	return op
}

func authorizeOperation(ctx context.Context, policy operationPolicy) error {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return err
	}
	if identityHasPermission(identity, policy.Permission) {
		return nil
	}
	return forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
}

func identityHasPermission(identity *auth.Identity, required permission) bool {
	if identity == nil || required == "" {
		return false
	}
	if identityHasDirectPermission(identity, required) {
		return true
	}
	for _, role := range identityRolesForCurrentOrg(identity) {
		for _, granted := range rolePermissionBundles[role] {
			if granted == required {
				return true
			}
		}
	}
	return false
}

func identityRolesForCurrentOrg(identity *auth.Identity) []string {
	if identity == nil {
		return nil
	}
	if len(identity.RoleAssignments) > 0 && identity.OrgID != "" {
		roles := make([]string, 0, len(identity.RoleAssignments))
		for _, assignment := range identity.RoleAssignments {
			if assignment.OrganizationID == identity.OrgID && assignment.Role != "" {
				roles = append(roles, assignment.Role)
			}
		}
		sort.Strings(roles)
		return compactStrings(roles)
	}
	roles := append([]string(nil), identity.Roles...)
	sort.Strings(roles)
	return compactStrings(roles)
}

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	requiredText := string(required)
	for _, claimKey := range []string{"scope", "scp", "permissions", "permission"} {
		for _, value := range stringClaimList(identity.Raw[claimKey]) {
			if value == requiredText {
				return true
			}
		}
	}
	return false
}

func stringClaimList(value any) []string {
	switch typed := value.(type) {
	case string:
		return strings.Fields(typed)
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return compactStrings(out)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, stringClaimList(item)...)
		}
		sort.Strings(out)
		return compactStrings(out)
	default:
		return nil
	}
}

func compactStrings(values []string) []string {
	out := values[:0]
	var previous string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

func publicAPIOperationIDs() []string {
	ids := make([]string, 0, len(publicAPIOperationPolicies))
	for operationID := range publicAPIOperationPolicies {
		ids = append(ids, operationID)
	}
	sort.Strings(ids)
	return ids
}

func applyPublicAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Zitadel-issued bearer token scoped to the sandbox-rental API audience.",
	}
}
