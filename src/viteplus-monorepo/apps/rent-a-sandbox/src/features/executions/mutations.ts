import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { entitlementsQuery } from "~/features/billing/queries";
import { submitDirectExecution, type ExecutionRequest } from "~/server-fns/api";

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

  return useMutation<CreateExecutionResult, Error, ExecutionRequest>({
    mutationFn: (data: ExecutionRequest) => submitDirectExecution({ data }),
    onSuccess: async (execution) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "executions") }),
        queryClient.invalidateQueries({ queryKey: entitlementsQuery(auth).queryKey }),
      ]);
      await onSuccess?.(execution);
    },
  });
}
