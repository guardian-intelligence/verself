import { REFERENCE_SECTIONS } from "./openapi-catalog";

// Single source of truth for the docs left rail. Top-level entries are
// doc pages; optional `children` are in-page anchors that expand under
// the active entry. Reference children are derived from the OpenAPI
// catalog so adding a service to SERVICE_CATALOG automatically surfaces
// it in the rail — no manual sync point.
export type DocsNavChild = {
  readonly id: string; // must match the DOM id the anchor scrolls to
  readonly label: string;
};

export type DocsNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly exactMatch?: boolean;
  readonly status?: "available" | "coming-soon";
  readonly description?: string;
  readonly children?: readonly DocsNavChild[];
};

export const DOCS_NAV: readonly DocsNavEntry[] = [
  {
    id: "overview",
    label: "Overview",
    to: "/docs",
    matchPrefix: "/docs",
    exactMatch: true,
    status: "available",
    description: "Platform orientation and product surface.",
  },
  {
    id: "getting-started",
    label: "Getting Started",
    to: "/docs/getting-started",
    matchPrefix: "/docs/getting-started",
    status: "coming-soon",
    description: "First run for the hosted sandbox product and the self-host bootstrap CLI.",
  },
  {
    id: "cli",
    label: "CLI",
    to: "/docs/cli",
    matchPrefix: "/docs/cli",
    status: "available",
    description: "Command reference for operating any Verself installation.",
    children: [
      { id: "overview", label: "Overview" },
      { id: "install", label: "Install" },
      { id: "authentication", label: "Authentication" },
      { id: "profiles", label: "Profiles" },
      { id: "resource-identifiers", label: "Resource identifiers" },
      { id: "organizations", label: "Organizations" },
      { id: "members", label: "Members" },
      { id: "credentials", label: "Credentials" },
      { id: "workload-trust", label: "Workload trust" },
      { id: "runner-onboarding", label: "Runner onboarding" },
      { id: "projects", label: "Projects" },
      { id: "repositories", label: "Repositories" },
      { id: "sandboxes", label: "Sandboxes" },
      { id: "secrets", label: "Secrets" },
      { id: "notifications", label: "Notifications" },
      { id: "governance", label: "Governance" },
      { id: "billing", label: "Billing" },
      { id: "self-hosting", label: "Self-hosting" },
      { id: "reference", label: "Reference" },
    ],
  },
  {
    id: "sdk",
    label: "SDK",
    to: "/docs/sdk",
    matchPrefix: "/docs/sdk",
    status: "available",
    description:
      "Curated TypeScript and Go facades over public Verself APIs, with machine authentication and customer-facing DTOs.",
    children: [
      { id: "overview", label: "Overview" },
      { id: "model", label: "Model" },
      { id: "installation", label: "Installation" },
      { id: "client-construction", label: "Client construction" },
      { id: "authentication", label: "Authentication" },
      { id: "credentials", label: "Credentials" },
      { id: "workload-trust", label: "Workload trust" },
      { id: "organization-context", label: "Organization context" },
      { id: "resource-identity", label: "Resource identity" },
      { id: "resources", label: "Resources" },
      { id: "api-shape", label: "API shape" },
      { id: "mutations", label: "Mutations" },
      { id: "pagination", label: "Pagination" },
      { id: "errors", label: "Errors" },
      { id: "observability", label: "Observability" },
      { id: "versioning", label: "Versioning" },
      { id: "language-support", label: "Language support" },
      { id: "standards", label: "Standards" },
      { id: "design-contract", label: "Design contract" },
    ],
  },
  {
    id: "governance",
    label: "Governance",
    to: "/docs/governance",
    matchPrefix: "/docs/governance",
    status: "available",
    description: "Audit events, high-risk activity views, and organization data exports.",
    children: [
      { id: "overview", label: "Overview" },
      { id: "audit-events", label: "Audit events" },
      { id: "data-exports", label: "Data exports" },
      { id: "commands", label: "Commands" },
    ],
  },
  {
    id: "notifications",
    label: "Notifications",
    to: "/docs/notifications",
    matchPrefix: "/docs/notifications",
    status: "available",
    description: "Inbox, notification preferences, read state, and test delivery.",
    children: [
      { id: "overview", label: "Overview" },
      { id: "resources", label: "Resources" },
      { id: "commands", label: "Commands" },
      { id: "events", label: "Events" },
    ],
  },
  {
    id: "sandboxes",
    label: "Sandboxes",
    to: "/docs/sandboxes",
    matchPrefix: "/docs/sandboxes",
    status: "coming-soon",
    description: "Short-lived Firecracker execution environments for customer workloads.",
  },
  {
    id: "functions",
    label: "Functions",
    to: "/docs/functions",
    matchPrefix: "/docs/functions",
    status: "coming-soon",
    description: "Lambda-style workload execution on the sandbox substrate.",
  },
  {
    id: "workflows",
    label: "Workflows",
    to: "/docs/workflows",
    matchPrefix: "/docs/workflows",
    status: "coming-soon",
    description: "Durable and scheduled execution patterns built on sandbox rentals.",
  },
  {
    id: "secrets-and-keys",
    label: "Secrets & Keys",
    to: "/docs/secrets",
    matchPrefix: "/docs/secrets",
    status: "available",
    description: "Secret and key material handling for sandbox workloads.",
    children: [
      { id: "overview", label: "Overview" },
      { id: "shared-responsibility", label: "Shared responsibility" },
      { id: "secrets", label: "Secrets" },
      { id: "keys", label: "Keys" },
      { id: "sandboxes", label: "Using in sandboxes" },
      { id: "access-control", label: "Access control" },
      { id: "audit-trail", label: "Audit trail" },
      { id: "limits", label: "Limits" },
    ],
  },
  {
    id: "pricing",
    label: "Pricing",
    to: "/docs/pricing",
    matchPrefix: "/docs/pricing",
    status: "coming-soon",
    description: "Compute metering, credits, billing windows, and invoices.",
  },
  {
    id: "security",
    label: "Security",
    to: "/docs/security",
    matchPrefix: "/docs/security",
    status: "coming-soon",
    description: "Isolation, workload identity, auditability, and data-retention commitments.",
  },
  {
    id: "iam-auth-errors",
    label: "IAM Auth Errors",
    to: "/docs/reference/iam/errors",
    matchPrefix: "/docs/reference/iam/errors",
    status: "available",
    description: "Stable account, provider-linking, device-session, and device-code auth errors.",
    children: [
      { id: "shape", label: "Shape" },
      { id: "authentication", label: "Authentication" },
      { id: "account-linking", label: "Account linking" },
      { id: "organization-context", label: "Organization context" },
      { id: "device-code", label: "Device code" },
    ],
  },
  {
    id: "github-integration-errors",
    label: "GitHub CI Errors",
    to: "/docs/reference/github-integration/errors",
    matchPrefix: "/docs/reference/github-integration/errors",
    status: "available",
    description: "Stable github-integration-service lifecycle problem codes and remediation links.",
    children: [
      { id: "shape", label: "Shape" },
      { id: "webhook-ingress", label: "Webhook ingress" },
      { id: "delivery-processing", label: "Delivery processing" },
      { id: "runner-capacity", label: "Runner capacity" },
      { id: "provider-surface", label: "Provider surface" },
    ],
  },
  {
    id: "reference",
    label: "API Reference",
    to: "/docs/reference",
    matchPrefix: "/docs/reference",
    status: "available",
    description: "Generated OpenAPI reference for public Verself service APIs.",
    children: REFERENCE_SECTIONS,
  },
];

export function isPathActive(
  currentPath: string,
  entry: { matchPrefix: string; exactMatch?: boolean },
): boolean {
  if (entry.exactMatch) {
    return currentPath === entry.matchPrefix;
  }
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
