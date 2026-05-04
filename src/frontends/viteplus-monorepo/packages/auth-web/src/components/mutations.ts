import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "../react.ts";
import { useIAMApi } from "./iam-api.ts";
import { invalidateOrganizationQueries } from "./queries.ts";
import type {
  InviteMemberRequest,
  PutMemberCapabilitiesRequest,
  UpdateOrganizationRequest,
  UpdateMemberRolesRequest,
} from "./types.ts";

export function useUpdateOrganizationMutation() {
  const auth = useSignedInAuth();
  const api = useIAMApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: UpdateOrganizationRequest) => api.updateOrganization(input),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function useInviteMemberMutation() {
  const auth = useSignedInAuth();
  const api = useIAMApi();
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
  const api = useIAMApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: UpdateMemberRolesRequest) => api.updateMemberRoles(input),
    onSuccess: async () => {
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}

export function usePutMemberCapabilitiesMutation() {
  const auth = useSignedInAuth();
  const api = useIAMApi();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: PutMemberCapabilitiesRequest) => api.putMemberCapabilities(input),
    onSuccess: async () => {
      // Server is the source of truth for the version bump; invalidating the
      // org subtree forces a refetch which re-keys the editor below
      // <CapabilitySection key={document.version}>.
      await invalidateOrganizationQueries(queryClient, auth);
    },
  });
}
