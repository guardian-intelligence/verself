import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "../react.ts";
import { useIdentityApi } from "./identity-api.ts";
import { invalidateOrganizationQueries } from "./queries.ts";
import type { InviteMemberRequest, PutPolicyRequest, UpdateMemberRolesRequest } from "./types.ts";

export function useInviteMemberMutation() {
  const auth = useSignedInAuth();
  const api = useIdentityApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: InviteMemberRequest) => api.inviteMember(input),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function useUpdateMemberRolesMutation() {
  const auth = useSignedInAuth();
  const api = useIdentityApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: UpdateMemberRolesRequest) => api.updateMemberRoles(input),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function usePutPolicyMutation() {
  const auth = useSignedInAuth();
  const api = useIdentityApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: PutPolicyRequest) => api.putPolicy(input),
    onSuccess: async () => {
      // Server is the source of truth for the version bump; invalidating
      // forces refetch which re-keys the editor (see <PolicyEditor key=
      // policy.version>).
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}
