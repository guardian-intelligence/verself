import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { listNotifications } from "~/server-fns/api";

export type NotificationsQueryScope = {
  readonly latestSequence: number;
  readonly limit?: number;
  readonly readUpToSequence: number;
  readonly revision: string;
};

export const notificationsQueryKey = (auth: AuthenticatedAuth) =>
  authQueryKey(auth, "notifications");

export const notificationsQuery = (auth: AuthenticatedAuth, scope: NotificationsQueryScope) => {
  const limit = scope.limit ?? 10;
  return queryOptions({
    queryKey: authQueryKey(
      auth,
      "notifications",
      limit,
      scope.latestSequence,
      scope.readUpToSequence,
      scope.revision,
    ),
    queryFn: () => listNotifications({ data: { limit } }),
    placeholderData: keepPreviousData,
    staleTime: 5_000,
  });
};
