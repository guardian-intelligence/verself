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
    description: "First run for the hosted sandbox product and clone bootstrap CLI.",
  },
  {
    id: "cli",
    label: "CLI",
    to: "/docs/cli",
    matchPrefix: "/docs/cli",
    status: "available",
    description: "Command reference for operating Verself and cloned installations.",
    children: [
      { id: "profiles", label: "Profiles" },
      { id: "organizations", label: "Organizations" },
      { id: "api-credentials", label: "API credentials" },
      { id: "projects", label: "Projects" },
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
