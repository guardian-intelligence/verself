import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { notFound } from "@tanstack/react-router";
import { getRepo, getRepoGenerations, getRepos } from "~/server-fns/api";

export function shouldPollRepo(state: string): boolean {
  return state === "importing" || state === "waiting_for_bootstrap" || state === "preparing";
}

export function shouldPollGeneration(state: string): boolean {
  return state === "queued" || state === "building" || state === "sanitizing";
}

export const reposQuery = () =>
  queryOptions({
    queryKey: ["repos"] as const,
    queryFn: () => getRepos(),
    refetchInterval: (query) => {
      const repos = query.state.data;
      if (!repos) return false;
      return repos.some((repo) => shouldPollRepo(repo.state)) ? 2_000 : false;
    },
  });

export const repoQuery = (repoId: string) =>
  queryOptions({
    queryKey: ["repos", repoId] as const,
    queryFn: () => getRepo({ data: { repoId } }),
    refetchInterval: (query) => {
      const repo = query.state.data;
      return repo && shouldPollRepo(repo.state) ? 2_000 : false;
    },
  });

export const repoGenerationsQuery = (repoId: string) =>
  queryOptions({
    queryKey: ["repos", repoId, "generations"] as const,
    queryFn: () => getRepoGenerations({ data: { repoId } }),
    refetchInterval: (query) => {
      const generations = query.state.data;
      if (!generations) return false;
      return generations.some((generation) => shouldPollGeneration(generation.state)) ? 2_000 : false;
    },
  });

export async function loadReposIndex(queryClient: QueryClient) {
  return queryClient.ensureQueryData(reposQuery());
}

export async function loadRepoDetail(queryClient: QueryClient, repoId: string) {
  try {
    await Promise.all([
      queryClient.ensureQueryData(repoQuery(repoId)),
      queryClient.ensureQueryData(repoGenerationsQuery(repoId)),
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

export function canRun(repo: { active_golden_generation_id?: string; state: string }) {
  return (repo.state === "ready" || repo.state === "degraded") && !!repo.active_golden_generation_id;
}

function sandboxRentalApiStatus(error: unknown): number | null {
  if (error instanceof Error) {
    const match = /^Sandbox rental API (\d+):/.exec(error.message);
    if (match) {
      return Number(match[1]);
    }
  }
  return null;
}

export function isSandboxRentalNotFound(error: unknown) {
  return sandboxRentalApiStatus(error) === 404;
}
