import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  dismissNotification,
  markNotificationRead,
  publishTestNotification,
  type DismissNotificationRequest,
  type MarkNotificationReadRequest,
  type PublishTestNotificationRequest,
} from "~/server-fns/api";
import { notificationsQuery } from "./queries";

export function useMarkNotificationReadMutation(latestSequence: number) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: MarkNotificationReadRequest) => markNotificationRead({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQuery(auth, latestSequence).queryKey,
      });
    },
  });
}

export function useDismissNotificationMutation(latestSequence: number) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: DismissNotificationRequest) => dismissNotification({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQuery(auth, latestSequence).queryKey,
      });
    },
  });
}

export function usePublishTestNotificationMutation(latestSequence: number) {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (body: PublishTestNotificationRequest) => publishTestNotification({ data: body }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: notificationsQuery(auth, latestSequence).queryKey,
      });
    },
  });
}
