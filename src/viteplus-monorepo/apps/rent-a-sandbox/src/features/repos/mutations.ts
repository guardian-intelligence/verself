import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuthState } from "@forge-metal/auth-web";
import { balanceQuery } from "~/features/billing/queries";
import { importRepo, refreshRepo, rescanRepo, submitRepoExecution } from "~/server-fns/api";
import { repoGenerationsQuery, repoQuery, reposQuery } from "./queries";

export async function invalidateRepoQueries(
  queryClient: QueryClient,
  authState: AuthenticatedAuthState,
  repoId?: string,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: reposQuery(authState).queryKey }),
    queryClient.invalidateQueries({ queryKey: authQueryKey(authState, "jobs") }),
    queryClient.invalidateQueries({ queryKey: balanceQuery(authState).queryKey }),
    ...(repoId
      ? [queryClient.invalidateQueries({ queryKey: repoQuery(authState, repoId).queryKey })]
      : []),
    ...(repoId
      ? [
          queryClient.invalidateQueries({
            queryKey: repoGenerationsQuery(authState, repoId).queryKey,
          }),
        ]
      : []),
  ]);
}

export function useImportRepoMutation(
  authState: AuthenticatedAuthState,
  onSuccess: (repoId: string) => void,
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { clone_url: string; default_branch: string }) => importRepo({ data }),
    onSuccess: async (repo) => {
      await invalidateRepoQueries(queryClient, authState);
      onSuccess(repo.repo_id);
    },
  });
}

export function useRescanRepoMutation(authState: AuthenticatedAuthState, repoId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => rescanRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, authState, repoId);
    },
  });
}

export function useRefreshRepoMutation(authState: AuthenticatedAuthState, repoId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => refreshRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, authState, repoId);
    },
  });
}

export function useRunRepoExecutionMutation(
  authState: AuthenticatedAuthState,
  repoId: string,
  onSuccess: (executionId: string) => void,
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => submitRepoExecution({ data: { repo_id: repoId } }),
    onSuccess: async (result) => {
      await invalidateRepoQueries(queryClient, authState, repoId);
      onSuccess(result.execution_id);
    },
  });
}
