import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { getProfile } from "~/server-fns/api";

export const profileQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: authQueryKey(auth, "profile"),
    queryFn: () => getProfile(),
    staleTime: 0,
  });

export async function loadProfilePage(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(profileQuery(auth));
}
