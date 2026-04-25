import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getExecution } from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";
import { isExecutionActiveStatus } from "./status";

export async function loadExecutionsIndex(
  _queryClient: QueryClient,
  _auth: AuthenticatedAuth,
): Promise<void> {
  return Promise.resolve();
}

export function executionQuery(auth: AuthenticatedAuth, executionId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "executions", executionId),
    queryFn: () => getExecution({ data: { executionId } }),
    staleTime: 10_000,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return isExecutionActiveStatus(status) ? 2_000 : false;
    },
  });
}

export async function loadExecutionDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  executionId: string,
) {
  return ensureOrNotFound(queryClient, executionQuery(auth, executionId));
}
