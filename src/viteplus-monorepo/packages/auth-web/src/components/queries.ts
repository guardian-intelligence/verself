import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "../isomorphic.ts";
import type { IAMApiClient } from "./iam-api.ts";

export interface OrganizationMetadataValue {
  readonly display_name: string;
  readonly slug: string;
}

function organizationQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "organization", ...parts);
}

export const organizationQuery = (auth: AuthenticatedAuth, api: IAMApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "summary"),
    queryFn: () => api.getOrganization(),
  });

export const availableOrganizationMetadataQuery = (auth: AuthenticatedAuth, api: IAMApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "available-metadata"),
    queryFn: async () => {
      const organizations = await api.listMyOrganizations();
      return new Map<string, OrganizationMetadataValue>(
        organizations.map((organization) => [
          organization.org_id,
          {
            display_name: organization.display_name,
            slug: organization.slug,
          },
        ]),
      );
    },
  });

export const organizationMembersQuery = (auth: AuthenticatedAuth, api: IAMApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "members"),
    queryFn: () => api.listMembers(),
  });

export const organizationMemberCapabilitiesQuery = (auth: AuthenticatedAuth, api: IAMApiClient) =>
  queryOptions({
    queryKey: organizationQueryKey(auth, "member-capabilities"),
    queryFn: () => api.getMemberCapabilities(),
  });

export async function loadOrganizationPage(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  api: IAMApiClient,
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
