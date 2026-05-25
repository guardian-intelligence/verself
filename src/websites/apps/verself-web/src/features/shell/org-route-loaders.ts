import { type QueryClient } from "@tanstack/react-query";
import { notFound, redirect } from "@tanstack/react-router";
import {
  availableOrganizationMetadataQuery,
  type OrganizationMetadataValue,
} from "@verself/auth-web/components";
import { type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { iamApiClient } from "~/lib/iam-api-client";
import { selectActiveOrganization } from "~/server-fns/auth";
import {
  accountSelectionLoginPath,
  resolveAuthenticatedShellFallbackPath,
  shellNotFoundFallbackData,
} from "./fallback-routing";
import { orgPath } from "./org-routing";

export type ActiveOrganizationRouteContext = {
  readonly orgID: string;
  readonly organization: OrganizationMetadataValue;
};

export async function resolveDefaultSignedInPath(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
): Promise<string> {
  const active = await resolveCurrentOrganization(queryClient, auth);
  return active ? orgPath(active.organization.slug, "/") : "/onboarding";
}

export async function loadActiveOrganizationRoute({
  auth,
  href,
  orgSlug,
  queryClient,
}: {
  readonly auth: AuthenticatedAuth;
  readonly href: string;
  readonly orgSlug: string;
  readonly queryClient: QueryClient;
}): Promise<ActiveOrganizationRouteContext> {
  const organizations = await queryClient.ensureQueryData(
    availableOrganizationMetadataQuery(auth, iamApiClient),
  );
  const match = Array.from(organizations.entries()).find(
    ([, organization]) => organization.slug === orgSlug,
  );
  if (!match) {
    if (organizations.size === 0) {
      throw redirect({ href: accountSelectionLoginPath(href), replace: true });
    }
    throw notFound({
      data: shellNotFoundFallbackData(resolveAuthenticatedShellFallbackPath(auth, organizations)),
    });
  }

  const [orgID, organization] = match;
  if (auth.selectedOrgId !== orgID) {
    await selectActiveOrganization({ data: { orgID } });
    throw redirect({ href, replace: true });
  }

  return { orgID, organization };
}

async function resolveCurrentOrganization(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
): Promise<ActiveOrganizationRouteContext | null> {
  const orgID = auth.selectedOrgId ?? auth.orgId;
  if (!orgID) {
    return null;
  }
  const organizations = await queryClient.ensureQueryData(
    availableOrganizationMetadataQuery(auth, iamApiClient),
  );
  const organization = organizations.get(orgID);
  if (!organization) {
    return null;
  }
  return { orgID, organization };
}
