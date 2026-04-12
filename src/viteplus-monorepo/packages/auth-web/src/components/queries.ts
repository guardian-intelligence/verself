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

export const organizationOperationsQuery = (auth: AuthenticatedAuth, api: IdentityApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "operations"),
    queryFn: () => api.listOperations(),
  });

export const organizationPolicyQuery = (auth: AuthenticatedAuth, api: IdentityApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "policy"),
    queryFn: () => api.getPolicy(),
  });

export async function loadOrganizationPage(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  api: IdentityApiClient,
) {
  const [organization, members, operations, policy] = await Promise.all([
    queryClient.ensureQueryData(organizationQuery(auth, api)),
    queryClient.ensureQueryData(organizationMembersQuery(auth, api)),
    queryClient.ensureQueryData(organizationOperationsQuery(auth, api)),
    queryClient.ensureQueryData(organizationPolicyQuery(auth, api)),
  ]);

  return { members, operations, organization, policy };
}

export async function invalidateOrganizationQueries(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
) {
  await queryClient.invalidateQueries({
    queryKey: organizationQueryKey(auth),
  });
}
