import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import {
  getSourceBlob,
  getSourceRepository,
  getSourceTree,
  listSourceRefs,
  listSourceRepositories,
} from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";

export function sourceRepositoriesQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories"),
    queryFn: () => listSourceRepositories(),
    staleTime: 10_000,
  });
}

export function sourceRepositoryQuery(auth: AuthenticatedAuth, repoId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "source-repositories", repoId),
    queryFn: () => getSourceRepository({ data: { repoId } }),
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

export function sourceTreeQuery(
  auth: AuthenticatedAuth,
  repoId: string,
  input: { ref?: string; path?: string },
) {
  return queryOptions({
    queryKey: authQueryKey(
      auth,
      "source-repositories",
      repoId,
      "tree",
      input.ref ?? "",
      input.path ?? "",
    ),
    queryFn: () => getSourceTree({ data: { repoId, ...input } }),
    staleTime: 10_000,
  });
}

export function sourceBlobQuery(
  auth: AuthenticatedAuth,
  repoId: string,
  input: { ref?: string; path: string },
) {
  return queryOptions({
    queryKey: authQueryKey(
      auth,
      "source-repositories",
      repoId,
      "blob",
      input.ref ?? "",
      input.path,
    ),
    queryFn: () => getSourceBlob({ data: { repoId, ...input } }),
    staleTime: 10_000,
  });
}

export async function loadSourceRepositories(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(sourceRepositoriesQuery(auth));
}

export async function loadSourceRepositoryDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  repoId: string,
) {
  await ensureOrNotFound(queryClient, sourceRepositoryQuery(auth, repoId));
  await queryClient.ensureQueryData(sourceRefsQuery(auth, repoId));
}
