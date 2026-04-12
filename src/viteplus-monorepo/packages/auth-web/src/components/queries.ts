import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "../isomorphic.ts";
import type { IdentityApiClient } from "./identity-api.ts";

function organizationQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "organization", ...parts);
}

export const organizationQuery = (auth: AuthenticatedAuth, api: IdentityApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "summary"),
    queryFn: () => api.getOrganization(),
  });

export const organizationMembersQuery = (auth: AuthenticatedAuth, api: IdentityApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "members"),
    queryFn: () => api.listMembers(),
  });

export const organizationMemberCapabilitiesQuery = (
  auth: AuthenticatedAuth,
  api: IdentityApiClient,
) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "member-capabilities"),
    queryFn: () => api.getMemberCapabilities(),
  });

export async function loadOrganizationPage(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  api: IdentityApiClient,
) {
  const [organization, members, memberCapabilities] = await Promise.all([
    queryClient.ensureQueryData(organizationQuery(auth, api)),
    queryClient.ensureQueryData(organizationMembersQuery(auth, api)),
    queryClient.ensureQueryData(organizationMemberCapabilitiesQuery(auth, api)),
  ]);

  return { members, memberCapabilities, organization };
}

export async function invalidateOrganizationQueries(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
) {
  await queryClient.invalidateQueries({
    queryKey: organizationQueryKey(auth),
  });
}
