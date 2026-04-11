import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getMembers, getOperations, getOrganization, getPolicy } from "~/server-fns/api";

function organizationQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "organization", ...parts);
}

export const organizationQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "summary"),
    queryFn: () => getOrganization(),
  });

export const organizationMembersQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "members"),
    queryFn: () => getMembers(),
  });

export const organizationOperationsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "operations"),
    queryFn: () => getOperations(),
  });

export const organizationPolicyQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "policy"),
    queryFn: () => getPolicy(),
  });

export async function loadOrganizationPage(queryClient: QueryClient, auth: AuthenticatedAuth) {
  const [organization, members, operations, policy] = await Promise.all([
    queryClient.ensureQueryData(organizationQuery(auth)),
    queryClient.ensureQueryData(organizationMembersQuery(auth)),
    queryClient.ensureQueryData(organizationOperationsQuery(auth)),
    queryClient.ensureQueryData(organizationPolicyQuery(auth)),
  ]);

  return {
    members,
    operations,
    organization,
    policy,
  };
}
