import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { listNotifications } from "~/server-fns/api";

export const notificationsQuery = (auth: AuthenticatedAuth, latestSequence: number) =>
  queryOptions({
    queryKey: authQueryKey(auth, "notifications", latestSequence),
    queryFn: () => listNotifications({ data: { limit: 10 } }),
    placeholderData: keepPreviousData,
    staleTime: 5_000,
  });
