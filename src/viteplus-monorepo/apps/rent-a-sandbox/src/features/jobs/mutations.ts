import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuthState } from "@forge-metal/auth-web";
import { balanceQuery } from "~/features/billing/queries";
import { submitRepoExecution, type RepoExecutionRequest } from "~/server-fns/api";

export interface CreateExecutionResult {
  execution_id: string;
  attempt_id: string;
  status: string;
}

export function useCreateExecutionMutation({
  authState,
  onSuccess,
}: {
  authState: AuthenticatedAuthState;
  onSuccess?: (execution: CreateExecutionResult) => void | Promise<void>;
}) {
  const queryClient = useQueryClient();

  return useMutation<CreateExecutionResult, Error, RepoExecutionRequest>({
    mutationFn: (data: RepoExecutionRequest) => submitRepoExecution({ data }),
    onSuccess: async (execution) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: authQueryKey(authState, "jobs") }),
        queryClient.invalidateQueries({ queryKey: balanceQuery(authState).queryKey }),
      ]);
      await onSuccess?.(execution);
    },
  });
}
