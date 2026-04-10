import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getRepo, getRepoGenerations, getRepos, isSandboxRentalNotFound } from "~/server-fns/api";

export function shouldPollRepo(state: string): boolean {
  return state === "importing" || state === "waiting_for_bootstrap" || state === "preparing";
}

export function shouldPollGeneration(state: string): boolean {
  return state === "queued" || state === "building" || state === "sanitizing";
}

function reposQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "repos", ...parts);
}

export const reposQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: reposQueryKey(auth),
    queryFn: () => getRepos(),
    refetchInterval: (query) => {
      const repos = query.state.data;
      if (!repos) return false;
      return repos.some((repo) => shouldPollRepo(repo.state)) ? 2_000 : false;
    },
  });

export const repoQuery = (auth: AuthenticatedAuth, repoId: string) =>
  queryOptions({
    queryKey: reposQueryKey(auth, repoId),
    queryFn: () => getRepo({ data: { repoId } }),
    refetchInterval: (query) => {
      const repo = query.state.data;
      return repo && shouldPollRepo(repo.state) ? 2_000 : false;
    },
  });

export const repoGenerationsQuery = (auth: AuthenticatedAuth, repoId: string) =>
  queryOptions({
    queryKey: reposQueryKey(auth, repoId, "generations"),
    queryFn: () => getRepoGenerations({ data: { repoId } }),
    refetchInterval: (query) => {
      const generations = query.state.data;
      if (!generations) return false;
      return generations.some((generation) => shouldPollGeneration(generation.state))
        ? 2_000
        : false;
    },
  });

export async function loadReposIndex(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(reposQuery(auth));
}

export async function loadRepoDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  repoId: string,
) {
  try {
    await Promise.all([
      queryClient.ensureQueryData(repoQuery(auth, repoId)),
      queryClient.ensureQueryData(repoGenerationsQuery(auth, repoId)),
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
