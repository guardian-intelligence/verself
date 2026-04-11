import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  inviteMember,
  putPolicy,
  updateMemberRoles,
  type InviteMemberRequest,
  type PutPolicyRequest,
  type UpdateMemberRolesRequest,
} from "~/server-fns/api";
import {
  organizationMembersQuery,
  organizationOperationsQuery,
  organizationPolicyQuery,
  organizationQuery,
} from "./queries";

async function invalidateOrganizationQueries(queryClient: QueryClient, auth: AuthenticatedAuth) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: organizationQuery(auth).queryKey }),
    queryClient.invalidateQueries({ queryKey: organizationMembersQuery(auth).queryKey }),
    queryClient.invalidateQueries({ queryKey: organizationOperationsQuery(auth).queryKey }),
    queryClient.invalidateQueries({ queryKey: organizationPolicyQuery(auth).queryKey }),
  ]);
}

export function useInviteMemberMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: InviteMemberRequest) => inviteMember({ data }),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function useUpdateMemberRolesMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: UpdateMemberRolesRequest) => updateMemberRoles({ data }),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function usePutPolicyMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: PutPolicyRequest) => putPolicy({ data }),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}
