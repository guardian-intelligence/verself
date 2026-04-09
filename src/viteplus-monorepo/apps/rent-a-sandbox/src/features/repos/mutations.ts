import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { importRepo, refreshRepo, rescanRepo, submitRepoExecution } from "~/server-fns/api";

export async function invalidateRepoQueries(queryClient: QueryClient, repoId?: string) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["repos"] }),
    queryClient.invalidateQueries({ queryKey: ["jobs"] }),
    queryClient.invalidateQueries({ queryKey: ["billing", "balance"] }),
    ...(repoId ? [queryClient.invalidateQueries({ queryKey: ["repos", repoId] })] : []),
    ...(repoId
      ? [
          queryClient.invalidateQueries({
            queryKey: ["repos", repoId, "generations"],
          }),
        ]
      : []),
  ]);
}

export function useImportRepoMutation(onSuccess: (repoId: string) => void) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { clone_url: string; default_branch: string }) => importRepo({ data }),
    onSuccess: async (repo) => {
      await invalidateRepoQueries(queryClient);
      onSuccess(repo.repo_id);
    },
  });
}

export function useRescanRepoMutation(repoId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => rescanRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, repoId);
    },
  });
}

export function useRefreshRepoMutation(repoId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => refreshRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, repoId);
    },
  });
}

export function useRunRepoExecutionMutation(
  repoId: string,
  onSuccess: (executionId: string) => void,
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => submitRepoExecution({ data: { repo_id: repoId } }),
    onSuccess: async (result) => {
      await invalidateRepoQueries(queryClient, repoId);
      onSuccess(result.execution_id);
    },
  });
}
