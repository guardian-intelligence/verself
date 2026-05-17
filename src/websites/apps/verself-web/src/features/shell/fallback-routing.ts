import { type OrganizationMetadataValue } from "@verself/auth-web/components";
import { type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { orgPath } from "./org-routing";

type ShellNotFoundFallbackData = {
  readonly shellFallbackPath: string;
};

export function shellNotFoundFallbackData(fallbackPath: string): ShellNotFoundFallbackData {
  return { shellFallbackPath: fallbackPath };
}

export function resolveShellFallbackPath({
  notFoundData,
  orgSlug,
}: {
  readonly notFoundData: unknown;
  readonly orgSlug: string | null;
}): string {
  return fallbackPathFromNotFoundData(notFoundData) ?? (orgSlug ? orgPath(orgSlug, "/") : "/login");
}

export function resolveAuthenticatedShellFallbackPath(
  auth: AuthenticatedAuth,
  organizations: ReadonlyMap<string, OrganizationMetadataValue>,
): string {
  const orgID = auth.selectedOrgId ?? auth.orgId;
  if (!orgID) return "/onboarding";
  const organization = organizations.get(orgID);
  return organization ? orgPath(organization.slug, "/") : "/onboarding";
}

function fallbackPathFromNotFoundData(data: unknown): string | null {
  if (!data || typeof data !== "object") return null;
  const fallbackPath = (data as { readonly shellFallbackPath?: unknown }).shellFallbackPath;
  return typeof fallbackPath === "string" && isRootLocalPath(fallbackPath) ? fallbackPath : null;
}

function isRootLocalPath(value: string): boolean {
  return (
    value.startsWith("/") && !value.startsWith("//") && !value.includes("?") && !value.includes("#")
  );
}
