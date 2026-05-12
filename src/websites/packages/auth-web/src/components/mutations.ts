import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "../react.ts";
import { useIAMApi } from "./iam-api.ts";
import { invalidateOrganizationQueries } from "./queries.ts";
import type {
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
