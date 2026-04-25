import { useMutation, useQueryClient } from "@tanstack/react-query";
import { authQueryKey } from "@verself/auth-web/isomorphic";
import { useSignedInAuth } from "@verself/auth-web/react";
import {
  createExecutionSchedule,
  pauseExecutionSchedule,
  resumeExecutionSchedule,
  type ExecutionSchedule,
  type ExecutionScheduleRequest,
} from "~/server-fns/api";

function invalidateScheduleQueries(
  queryClient: QueryClient,
  auth: ReturnType<typeof useSignedInAuth>,
) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "execution-schedules") }),
    queryClient.invalidateQueries({ queryKey: authQueryKey(auth, "executions") }),
  ]);
}

type QueryClient = import("@tanstack/react-query").QueryClient;

export function useCreateExecutionScheduleMutation({
  onSuccess,
}: {
  onSuccess?: (schedule: ExecutionSchedule) => void | Promise<void>;
}) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ExecutionSchedule, Error, ExecutionScheduleRequest>({
    mutationFn: (data) => createExecutionSchedule({ data }),
    onSuccess: async (schedule) => {
      await invalidateScheduleQueries(queryClient, auth);
      await onSuccess?.(schedule);
    },
  });
}

export function usePauseExecutionScheduleMutation(scheduleId: string) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ExecutionSchedule, Error>({
    mutationFn: () => pauseExecutionSchedule({ data: { scheduleId } }),
    onSuccess: async () => {
      await invalidateScheduleQueries(queryClient, auth);
      await queryClient.invalidateQueries({
        queryKey: authQueryKey(auth, "execution-schedules", scheduleId),
      });
    },
  });
}

export function useResumeExecutionScheduleMutation(scheduleId: string) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation<ExecutionSchedule, Error>({
    mutationFn: () => resumeExecutionSchedule({ data: { scheduleId } }),
    onSuccess: async () => {
      await invalidateScheduleQueries(queryClient, auth);
      await queryClient.invalidateQueries({
        queryKey: authQueryKey(auth, "execution-schedules", scheduleId),
      });
    },
  });
}
