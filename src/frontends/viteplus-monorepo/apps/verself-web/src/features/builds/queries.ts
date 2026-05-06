import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { listRuns } from "~/server-fns/api";
import { DEFAULT_BUILD_RUNS_QUERY, normalizeBuildRunsQuery } from "./model";
import type { RunListQueryInput } from "~/lib/sandbox-rental-api";

export function buildRunsQuery(
  auth: AuthenticatedAuth,
  query: RunListQueryInput = DEFAULT_BUILD_RUNS_QUERY,
) {
  const normalizedQuery = normalizeBuildRunsQuery(query);
  return queryOptions({
    queryKey: authQueryKey(auth, "build-runs", normalizedQuery),
    queryFn: () => listRuns({ data: normalizedQuery }),
    staleTime: 5_000,
    refetchInterval: 5_000,
  });
}

export async function loadBuildsDashboard(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(buildRunsQuery(auth));
}
