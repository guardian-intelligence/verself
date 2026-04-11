import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getRepo, getRepos, isSandboxRentalNotFound } from "~/server-fns/api";

export function shouldPollRepo(state: string): boolean {
  return state === "importing";
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

export async function loadReposIndex(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(reposQuery(auth));
}

export async function loadRepoDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  repoId: string,
) {
  try {
    await queryClient.ensureQueryData(repoQuery(auth, repoId));
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

  return repo.state !== "archived" && repo.state !== "importing";
}
