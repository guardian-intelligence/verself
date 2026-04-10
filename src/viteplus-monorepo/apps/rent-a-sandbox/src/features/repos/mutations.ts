import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/shared";
import { useAuthenticatedAuth } from "@forge-metal/auth-web/react";
import { balanceQuery } from "~/features/billing/queries";
import { importRepo, refreshRepo, rescanRepo, submitRepoExecution } from "~/server-fns/api";
import { repoGenerationsQuery, repoQuery, reposQuery } from "./queries";

export async function invalidateRepoQueries(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  repoId?: string,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: reposQuery(auth).queryKey }),
    queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "jobs") }),
    queryClient.invalidateQueries({ queryKey: balanceQuery(auth).queryKey }),
    ...(repoId
      ? [queryClient.invalidateQueries({ queryKey: repoQuery(auth, repoId).queryKey })]
      : []),
    ...(repoId
      ? [
          queryClient.invalidateQueries({
            queryKey: repoGenerationsQuery(auth, repoId).queryKey,
          }),
        ]
      : []),
  ]);
}

export function useImportRepoMutation(onSuccess: (repoId: string) => void) {
  const auth = useAuthenticatedAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { clone_url: string; default_branch: string }) => importRepo({ data }),
    onSuccess: async (repo) => {
      await invalidateRepoQueries(queryClient, auth);
      onSuccess(repo.repo_id);
    },
  });
}

export function useRescanRepoMutation(repoId: string) {
  const auth = useAuthenticatedAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => rescanRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, auth, repoId);
    },
  });
}

export function useRefreshRepoMutation(repoId: string) {
  const auth = useAuthenticatedAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => refreshRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, auth, repoId);
    },
  });
}

export function useRunRepoExecutionMutation(
  repoId: string,
  onSuccess: (executionId: string) => void,
) {
  const auth = useAuthenticatedAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => submitRepoExecution({ data: { repo_id: repoId } }),
    onSuccess: async (result) => {
      await invalidateRepoQueries(queryClient, auth, repoId);
      onSuccess(result.execution_id);
    },
  });
}
