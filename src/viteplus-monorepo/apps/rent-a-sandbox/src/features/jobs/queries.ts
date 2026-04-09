import { queryOptions } from "@tanstack/react-query";
import { getBalance, getExecution } from "~/server-fns/api";
import { isExecutionActiveStatus } from "./status";

export function jobsBalanceQuery() {
  return queryOptions({
    queryKey: ["jobs", "balance"] as const,
    queryFn: () => getBalance(),
    staleTime: 10_000,
  });
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
