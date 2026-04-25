import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  createSourceRepository,
  type CreateSourceRepositoryRequest,
  type SourceRepository,
} from "~/server-fns/api";

export function useCreateSourceRepositoryMutation({
  onSuccess,
}: {
  onSuccess?: (repo: SourceRepository) => void | Promise<void>;
}) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<SourceRepository, Error, CreateSourceRepositoryRequest>({
    mutationFn: (data) => createSourceRepository({ data }),
    onSuccess: async (repo) => {
      await queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "source-repositories") });
      await onSuccess?.(repo);
    },
  });
}
