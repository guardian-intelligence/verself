import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { putProfilePreferences, updateProfileIdentity } from "~/server-fns/api";
import type {
  ProfileSnapshot,
  PutProfilePreferencesRequest,
  UpdateProfileIdentityRequest,
} from "~/server-fns/api";
import { profileQuery } from "./queries";

export function useUpdateProfileIdentityMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ProfileSnapshot, Error, UpdateProfileIdentityRequest>({
    mutationFn: (body) => updateProfileIdentity({ data: body }),
    onSuccess: async (profile) => {
      queryClient.setQueryData(profileQuery(auth).queryKey, profile);
      await queryClient.invalidateQueries({ queryKey: profileQuery(auth).queryKey });
    },
  });
}

export function usePutProfilePreferencesMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ProfileSnapshot, Error, PutProfilePreferencesRequest>({
    mutationFn: (body) => putProfilePreferences({ data: body }),
    onSuccess: async (profile) => {
      queryClient.setQueryData(profileQuery(auth).queryKey, profile);
      await queryClient.invalidateQueries({ queryKey: profileQuery(auth).queryKey });
    },
  });
}
