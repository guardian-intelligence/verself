import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import {
  getExecutionSchedule,
  listExecutionSchedules,
  type ExecutionScheduleListQueryInput,
} from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";

export function executionSchedulesQuery(
  auth: AuthenticatedAuth,
  query?: ExecutionScheduleListQueryInput,
) {
  return queryOptions({
    queryKey:
      query === undefined
        ? authQueryKey(auth, "execution-schedules")
        : authQueryKey(auth, "execution-schedules", query),
    queryFn: () =>
      query === undefined ? listExecutionSchedules() : listExecutionSchedules({ data: query }),
    staleTime: 10_000,
    refetchInterval: 5_000,
  });
}

export function executionScheduleQuery(auth: AuthenticatedAuth, scheduleId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "execution-schedules", scheduleId),
    queryFn: () => getExecutionSchedule({ data: { scheduleId } }),
    staleTime: 10_000,
    refetchInterval: 5_000,
  });
}

export async function loadExecutionSchedules(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(executionSchedulesQuery(auth));
}

export async function loadExecutionScheduleDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  scheduleId: string,
) {
  return ensureOrNotFound(queryClient, executionScheduleQuery(auth, scheduleId));
}
