import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { listCacheGenerations, listCaches, listRuns } from "~/server-fns/api";

// The console is a single ambient surface: one card per connected repo, each
// showing its recent CI runs. We pull a generous page of recent runs and group
// them client-side; resync is a 5s poll (TanStack Query is the only state).
const CONSOLE_RUNS_LIMIT = 50;

export function consoleRunsQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, "console-runs"),
    queryFn: () => listRuns({ data: { limit: CONSOLE_RUNS_LIMIT } }),
    staleTime: 5_000,
    refetchInterval: 5_000,
  });
}

export function consoleCachesQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, "console-caches"),
    queryFn: () => listCaches(),
    staleTime: 10_000,
    refetchInterval: 10_000,
  });
}

export function consoleCacheGenerationsQuery(auth: AuthenticatedAuth, cacheId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "console-cache-generations", cacheId),
    queryFn: () => listCacheGenerations({ data: { cacheId } }),
    staleTime: 10_000,
    refetchInterval: 10_000,
  });
}

export async function loadConsole(queryClient: QueryClient, auth: AuthenticatedAuth) {
  await Promise.all([
    queryClient.ensureQueryData(consoleRunsQuery(auth)),
    queryClient.ensureQueryData(consoleCachesQuery(auth)),
  ]);
}
