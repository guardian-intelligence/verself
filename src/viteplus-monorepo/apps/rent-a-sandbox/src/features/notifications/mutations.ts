import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  clearNotifications,
  dismissNotification,
  markNotificationRead,
  publishTestNotification,
  type DismissNotificationRequest,
  type MarkNotificationReadRequest,
  type PublishTestNotificationRequest,
} from "~/server-fns/api";
import { notificationsQueryKey } from "./queries";

export function useMarkNotificationReadMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: MarkNotificationReadRequest) => markNotificationRead({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQueryKey(auth),
      });
    },
  });
}

export function useDismissNotificationMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: DismissNotificationRequest) => dismissNotification({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQueryKey(auth),
      });
    },
  });
}

export function useClearNotificationsMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => clearNotifications(),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQueryKey(auth),
      });
    },
  });
}

export function usePublishTestNotificationMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: PublishTestNotificationRequest) => publishTestNotification({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQueryKey(auth),
      });
    },
  });
}
