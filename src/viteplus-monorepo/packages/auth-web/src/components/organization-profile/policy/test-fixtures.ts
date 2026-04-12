import type { Operations } from "../../types.ts";

// Mirror of identity-service/internal/identity/catalog.go defaultOperations.
// Kept here as a literal so the policy package has zero runtime coupling to
// the live API; the property tests get to drive the same operation set the
// server actually publishes. Update both files together when the catalog
// gains or loses an operation.
export const fixtureOperations: Operations = {
  services: [
    {
      service: "identity-service",
      operations: [
        op("get-organization", "identity:organization:read", "organization", "read"),
        op("list-organization-members", "identity:member:read", "organization_member", "list"),
        op("invite-organization-member", "identity:member:invite", "organization_member", "invite"),
        op(
          "update-organization-member-roles",
          "identity:member:roles:write",
          "organization_member_roles",
          "write",
        ),
        op("get-organization-policy", "identity:policy:read", "organization_policy", "read"),
        op("put-organization-policy", "identity:policy:write", "organization_policy", "write"),
        op("list-organization-operations", "identity:operations:read", "service_operation", "list"),
        op("list-api-credentials", "identity:api_credentials:read", "api_credential", "list"),
        op("get-api-credential", "identity:api_credentials:read", "api_credential", "read"),
        op("create-api-credential", "identity:api_credentials:create", "api_credential", "create"),
        op("roll-api-credential", "identity:api_credentials:roll", "api_credential", "roll"),
        op("revoke-api-credential", "identity:api_credentials:revoke", "api_credential", "revoke"),
      ],
    },
    {
      service: "sandbox-rental-service",
      operations: [
        op("import-repo", "sandbox:repo:write", "repo", "import"),
        op("list-repos", "sandbox:repo:read", "repo", "list"),
        op("get-repo", "sandbox:repo:read", "repo", "read"),
        op("rescan-repo", "sandbox:repo:write", "repo", "rescan"),
        op(
          "create-webhook-endpoint",
          "sandbox:webhook_endpoint:write",
          "webhook_endpoint",
          "create",
        ),
        op("list-webhook-endpoints", "sandbox:webhook_endpoint:read", "webhook_endpoint", "list"),
        op(
          "rotate-webhook-endpoint-secret",
          "sandbox:webhook_endpoint:write",
          "webhook_endpoint_secret",
          "rotate",
        ),
        op(
          "delete-webhook-endpoint",
          "sandbox:webhook_endpoint:write",
          "webhook_endpoint",
          "delete",
        ),
        op("submit-execution", "sandbox:execution:submit", "execution", "submit"),
        op("get-execution", "sandbox:execution:read", "execution", "read"),
        op("get-execution-logs", "sandbox:logs:read", "execution_logs", "read"),
        op("get-billing-balance", "billing:read", "billing_balance", "read"),
        op("list-billing-subscriptions", "billing:read", "billing_subscription", "list"),
        op("list-billing-grants", "billing:read", "billing_grant", "list"),
        op("get-billing-statement", "billing:read", "billing_statement", "read"),
        op("create-billing-checkout", "billing:checkout", "billing_checkout", "create"),
        op(
          "create-billing-subscription",
          "billing:checkout",
          "billing_subscription_checkout",
          "create",
        ),
        op("create-billing-portal", "billing:checkout", "billing_portal", "create"),
      ],
    },
  ],
};

export const knownPermissions: readonly string[] = Array.from(
  new Set(
    fixtureOperations.services.flatMap((service) =>
      service.operations.map((operation) => operation.permission),
    ),
  ),
).sort();

function op(operation_id: string, permission: string, resource: string, action: string) {
  return { operation_id, permission, resource, action, org_scope: "token_org_id" };
}
