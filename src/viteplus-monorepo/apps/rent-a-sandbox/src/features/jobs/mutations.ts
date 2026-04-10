import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { balanceQuery } from "~/features/billing/queries";
import { submitRepoExecution, type RepoExecutionRequest } from "~/server-fns/api";

export interface CreateExecutionResult {
  execution_id: string;
  attempt_id: string;
  status: string;
}

export function useCreateExecutionMutation({
  onSuccess,
}: {
  onSuccess?: (execution: CreateExecutionResult) => void | Promise<void>;
}) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<CreateExecutionResult, Error, RepoExecutionRequest>({
    mutationFn: (data: RepoExecutionRequest) => submitRepoExecution({ data }),
    onSuccess: async (execution) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "jobs") }),
        queryClient.invalidateQueries({ queryKey: balanceQuery(auth).queryKey }),
      ]);
      await onSuccess?.(execution);
    },
  });
}
