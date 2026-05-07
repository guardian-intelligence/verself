import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@verself/auth-web/react";
import {
  clearNotifications,
  dismissNotification,
  markNotificationRead,
  markNotificationReadByID,
  publishTestNotification,
  type DismissNotificationRequest,
  type MarkNotificationReadRequest,
  type PublishTestNotificationRequest,
} from "~/server-fns/api";
import { withInteractionSpan } from "~/lib/telemetry/interaction";
import { notificationsQueryKey } from "./queries";

export function useMarkNotificationReadMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: withInteractionSpan(
      "notifications.mark_read",
      (body: MarkNotificationReadRequest) => markNotificationRead({ data: body }),
    ),
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
    mutationFn: withInteractionSpan("notifications.dismiss", (body: DismissNotificationRequest) =>
      dismissNotification({ data: body }),
    ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQueryKey(auth),
      });
    },
  });
}

export function useMarkSingleNotificationReadMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: DismissNotificationRequest) => markNotificationReadByID({ data: body }),
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
