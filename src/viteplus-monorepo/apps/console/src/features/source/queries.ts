import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { listSourceRepositories, listSourceWorkflowRuns } from "~/server-fns/api";

export function sourceRepositoriesQuery(auth: AuthenticatedAuth, projectId?: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories", projectId ?? "all"),
    queryFn: () => listSourceRepositories({ data: projectId ? { projectId } : undefined }),
    staleTime: 10_000,
  });
}

export function sourceWorkflowRunsQuery(auth: AuthenticatedAuth, repoId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories", repoId, "workflow-runs"),
    queryFn: () => listSourceWorkflowRuns({ data: { repoId } }),
    staleTime: 10_000,
  });
}

export async function loadSourceRepositories(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(sourceRepositoriesQuery(auth));
}

export async function loadBuildsDashboard(queryClient: QueryClient, auth: AuthenticatedAuth) {
  const repositories = await queryClient.ensureQueryData(sourceRepositoriesQuery(auth));
  await Promise.all(
    repositories.repositories.map((repo) =>
      queryClient.ensureQueryData(sourceWorkflowRunsQuery(auth, repo.repo_id)),
    ),
  );
  return { repositories };
}
