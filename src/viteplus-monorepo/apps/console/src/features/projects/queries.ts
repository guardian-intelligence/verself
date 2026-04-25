import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { listProjects } from "~/server-fns/api";

export function projectsQuery(auth: AuthenticatedAuth) {
  return queryOptions({
    queryKey: authQueryKey(auth, "projects"),
    queryFn: () => listProjects(),
    staleTime: 10_000,
  });
}

export async function loadProjects(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(projectsQuery(auth));
}
