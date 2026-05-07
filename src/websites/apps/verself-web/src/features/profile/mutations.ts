import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@verself/auth-web/react";
import type { AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { putProfilePreferences, updateProfileIdentity } from "~/server-fns/api";
import type {
  ProfileSnapshot,
  PutProfilePreferencesRequest,
  UpdateProfileIdentityRequest,
} from "~/server-fns/api";
import { profileQuery } from "./queries";

export const profileMutationKeys = {
  all: (auth: AuthenticatedAuth) => [...profileQuery(auth).queryKey, "mutation"],
  identity: (auth: AuthenticatedAuth) => [...profileMutationKeys.all(auth), "identity"],
  preferences: (auth: AuthenticatedAuth) => [...profileMutationKeys.all(auth), "preferences"],
} as const;

export function useUpdateProfileIdentityMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ProfileSnapshot, Error, UpdateProfileIdentityRequest>({
    mutationKey: profileMutationKeys.identity(auth),
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
    mutationKey: profileMutationKeys.preferences(auth),
    mutationFn: (body) => putProfilePreferences({ data: body }),
    onSuccess: async (profile) => {
      queryClient.setQueryData(profileQuery(auth).queryKey, profile);
      await queryClient.invalidateQueries({ queryKey: profileQuery(auth).queryKey });
    },
  });
}
