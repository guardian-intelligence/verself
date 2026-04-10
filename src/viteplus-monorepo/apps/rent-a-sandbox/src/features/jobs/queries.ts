import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuthState } from "@forge-metal/auth-web";
import { loadBalance } from "~/features/billing/queries";
import { getExecution } from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";
import { isExecutionActiveStatus } from "./status";

export async function loadJobsIndex(queryClient: QueryClient, authState: AuthenticatedAuthState) {
  return loadBalance(queryClient, authState);
}

export function executionQuery(authState: AuthenticatedAuthState, executionId: string) {
  return queryOptions({
    queryKey: authQueryKey(authState, "jobs", executionId),
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
  authState: AuthenticatedAuthState,
  executionId: string,
) {
  return ensureOrNotFound(queryClient, executionQuery(authState, executionId));
}
