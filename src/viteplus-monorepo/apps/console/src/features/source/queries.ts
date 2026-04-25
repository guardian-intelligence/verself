import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { listSourceRefs, listSourceRepositories } from "~/server-fns/api";

export function sourceRepositoriesQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories"),
    queryFn: () => listSourceRepositories(),
    staleTime: 10_000,
  });
}

export function sourceRefsQuery(auth: AuthenticatedAuth, repoId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories", repoId, "refs"),
    queryFn: () => listSourceRefs({ data: { repoId } }),
    staleTime: 10_000,
  });
}

export async function loadSourceRepositories(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(sourceRepositoriesQuery(auth));
}

export async function loadSourceDashboard(queryClient: QueryClient, auth: AuthenticatedAuth) {
  const repositories = await queryClient.ensureQueryData(sourceRepositoriesQuery(auth));
  await Promise.all(
    repositories.repositories.map((repo) =>
      queryClient.ensureQueryData(sourceRefsQuery(auth, repo.repo_id)),
    ),
  );
  return repositories;
}
