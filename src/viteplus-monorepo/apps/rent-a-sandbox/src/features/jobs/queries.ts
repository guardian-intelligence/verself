import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { loadBalance } from "~/features/billing/queries";
import { getExecution } from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";
import { isExecutionActiveStatus } from "./status";

export async function loadJobsIndex(queryClient: QueryClient) {
  return loadBalance(queryClient);
}

export function executionQuery(executionId: string) {
  return queryOptions({
    queryKey: ["jobs", executionId] as const,
    queryFn: () => getExecution({ data: { executionId } }),
    staleTime: 10_000,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return isExecutionActiveStatus(status) ? 2_000 : false;
    },
  });
}

export async function loadExecutionDetail(queryClient: QueryClient, executionId: string) {
  return ensureOrNotFound(queryClient, executionQuery(executionId));
}
