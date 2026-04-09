import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  submitRepoExecution,
  type RepoExecutionRequest,
} from "~/server-fns/api";

export interface CreateExecutionResult {
  execution_id: string;
  attempt_id: string;
  status: string;
}

export function useCreateExecutionMutation({
  onSuccess,
}: {
  onSuccess?: (execution: CreateExecutionResult) => void | Promise<void>;
} = {}) {
  const queryClient = useQueryClient();

  return useMutation<CreateExecutionResult, Error, RepoExecutionRequest>({
    mutationFn: (data: RepoExecutionRequest) => submitRepoExecution({ data }),
    onSuccess: async (execution) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["jobs"] }),
        queryClient.invalidateQueries({ queryKey: ["billing", "balance"] }),
      ]);
      await onSuccess?.(execution);
    },
  });
}
