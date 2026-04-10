import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { authQueryKey, type AuthenticatedAuthState } from "@forge-metal/auth-web";
import { getRepo, getRepoGenerations, getRepos, isSandboxRentalNotFound } from "~/server-fns/api";

export function shouldPollRepo(state: string): boolean {
  return state === "importing" || state === "waiting_for_bootstrap" || state === "preparing";
}

export function shouldPollGeneration(state: string): boolean {
  return state === "queued" || state === "building" || state === "sanitizing";
}

function reposQueryKey<TParts extends readonly unknown[]>(
  authState: AuthenticatedAuthState,
  ...parts: TParts
) {
  return authQueryKey(authState, "repos", ...parts);
}

export const reposQuery = (authState: AuthenticatedAuthState) =>
  queryOptions({
    queryKey: reposQueryKey(authState),
    queryFn: () => getRepos(),
    refetchInterval: (query) => {
      const repos = query.state.data;
      if (!repos) return false;
      return repos.some((repo) => shouldPollRepo(repo.state)) ? 2_000 : false;
    },
  });

export const repoQuery = (authState: AuthenticatedAuthState, repoId: string) =>
  queryOptions({
    queryKey: reposQueryKey(authState, repoId),
    queryFn: () => getRepo({ data: { repoId } }),
    refetchInterval: (query) => {
      const repo = query.state.data;
      return repo && shouldPollRepo(repo.state) ? 2_000 : false;
    },
  });

export const repoGenerationsQuery = (authState: AuthenticatedAuthState, repoId: string) =>
  queryOptions({
    queryKey: reposQueryKey(authState, repoId, "generations"),
    queryFn: () => getRepoGenerations({ data: { repoId } }),
    refetchInterval: (query) => {
      const generations = query.state.data;
      if (!generations) return false;
      return generations.some((generation) => shouldPollGeneration(generation.state))
        ? 2_000
        : false;
    },
  });

export async function loadReposIndex(queryClient: QueryClient, authState: AuthenticatedAuthState) {
  return queryClient.ensureQueryData(reposQuery(authState));
}

export async function loadRepoDetail(
  queryClient: QueryClient,
  authState: AuthenticatedAuthState,
  repoId: string,
) {
  try {
    await Promise.all([
      queryClient.ensureQueryData(repoQuery(authState, repoId)),
      queryClient.ensureQueryData(repoGenerationsQuery(authState, repoId)),
    ]);
  } catch (error) {
    if (isSandboxRentalNotFound(error)) {
      throw notFound();
    }
    throw error;
  }
}

export function canRefresh(repo: { compatibility_status: string; state: string }) {
  if (repo.compatibility_status !== "compatible") {
    return false;
  }

  return repo.state !== "archived" && repo.state !== "importing" && repo.state !== "preparing";
}

export function canRun(repo: { active_golden_generation_id?: string | undefined; state: string }) {
  return (
    (repo.state === "ready" || repo.state === "degraded") && !!repo.active_golden_generation_id
  );
}
