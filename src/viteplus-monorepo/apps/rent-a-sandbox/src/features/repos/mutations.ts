import { type QueryClient, useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { balanceQuery } from "~/features/billing/queries";
import { importRepo, rescanRepo } from "~/server-fns/api";
import { repoQuery, reposQuery } from "./queries";

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
  ]);
}

export function useImportRepoMutation(onSuccess: (repoId: string) => void) {
  const auth = useSignedInAuth();
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
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => rescanRepo({ data: { repoId } }),
    onSuccess: async () => {
      await invalidateRepoQueries(queryClient, auth, repoId);
    },
  });
}
