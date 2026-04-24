import { useQuery } from "@tanstack/react-query";
import { Bell, CheckCheck, Loader2, Send, X } from "lucide-react";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { ElapsedTime } from "@forge-metal/ui/components/elapsed-time";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@forge-metal/ui/components/ui/popover";
import { Skeleton } from "@forge-metal/ui/components/ui/skeleton";
import { cn } from "@forge-metal/ui";
import { ErrorCallout } from "~/components/error-callout";
import { formatDateTimeUTC } from "~/lib/format";
import type { Notification } from "~/server-fns/api";
import { useNotificationInboxState } from "./live";
import {
  useDismissNotificationMutation,
  useMarkNotificationReadMutation,
  usePublishTestNotificationMutation,
} from "./mutations";
import { notificationsQuery } from "./queries";

export function NotificationBell() {
  const auth = useSignedInAuth();
  const live = useNotificationInboxState();
  const query = useQuery(notificationsQuery(auth, live.latestSequence));
  const summary = query.data?.summary;
  const notifications = query.data?.notifications ?? [];
  const latestSequence = Math.max(live.latestSequence, numericSequence(summary?.latest_sequence));
  const readUpToSequence = Math.max(
    live.readUpToSequence,
    numericSequence(summary?.read_up_to_sequence),
  );
  const unreadCount = Math.min(
    Math.max(live.unreadCount, summary?.unread_count ?? 0, latestSequence - readUpToSequence),
    999,
  );
  const unreadBadgeLabel = unreadCount > 99 ? "!" : String(unreadCount);
  const markRead = useMarkNotificationReadMutation(latestSequence);
  const dismiss = useDismissNotificationMutation(latestSequence);
  const test = usePublishTestNotificationMutation(latestSequence);
  const busy = query.isFetching || markRead.isPending || dismiss.isPending || test.isPending;
  const canMarkRead = latestSequence > readUpToSequence;

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Notifications"
            data-testid="notifications-bell"
            className="relative"
          >
            <Bell />
            {unreadCount > 0 ? (
              <span
                data-testid="notifications-unread-count"
                className="absolute -right-1 -top-1 flex min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-4 text-primary-foreground tabular-nums"
              >
                {unreadBadgeLabel}
              </span>
            ) : null}
          </Button>
        }
      />
      <PopoverContent
        align="end"
        sideOffset={8}
        className="w-96 max-w-[calc(100vw-2rem)] gap-0 overflow-hidden rounded-md p-0"
        data-testid="notifications-popover"
      >
        <PopoverHeader className="flex-row items-center justify-between gap-3 border-b px-3 py-2">
          <div className="min-w-0">
            <PopoverTitle>Notifications</PopoverTitle>
            <div className="text-xs text-muted-foreground tabular-nums">
              {unreadCount > 0 ? `${unreadCount} unread` : "No unread"}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {busy ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="Send test notification"
              title="Send test notification"
              data-testid="notifications-test"
              onClick={() => test.mutate({ title: "Notification test" })}
            >
              <Send />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label="Mark notifications read"
              title="Mark notifications read"
              data-testid="notifications-mark-read"
              aria-busy={markRead.isPending}
              onClick={() => {
                if (!canMarkRead) return;
                markRead.mutate({ read_up_to_sequence: String(Math.max(latestSequence, 0)) });
              }}
            >
              <CheckCheck />
            </Button>
          </div>
        </PopoverHeader>
        <div className="max-h-96 overflow-y-auto">
          {query.isError ? (
            <div className="p-3" data-testid="notifications-error">
              <ErrorCallout title="Notifications unavailable" error={query.error} />
            </div>
          ) : query.isPending ? (
            <LoadingRows />
          ) : notifications.length === 0 ? (
            <div className="px-3 py-8 text-center text-sm text-muted-foreground">
              No notifications
            </div>
          ) : (
            <div className="divide-y">
              {notifications.map((notification) => (
                <NotificationRow
                  key={notification.notification_id}
                  notification={notification}
                  read={numericSequence(notification.recipient_sequence) <= readUpToSequence}
                  onDismiss={() =>
                    dismiss.mutate({ notification_id: notification.notification_id })
                  }
                />
              ))}
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function NotificationBellFallback() {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label="Notifications"
      aria-busy="true"
      data-testid="notifications-bell-fallback"
    >
      <Bell />
    </Button>
  );
}

function LoadingRows() {
  return (
    <div className="space-y-3 p-3" data-testid="notifications-loading">
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-full" />
    </div>
  );
}

function NotificationRow({
  notification,
  read,
  onDismiss,
}: {
  notification: Notification;
  read: boolean;
  onDismiss: () => void;
}) {
  return (
    <article
      className={cn(
        "grid grid-cols-[1fr_auto] gap-2 px-3 py-2.5 transition-colors duration-200 data-[new=true]:animate-in data-[new=true]:fade-in-0 data-[new=true]:slide-in-from-top-1",
        !read && "bg-muted/40",
      )}
      data-testid="notification-row"
      data-notification-id={notification.notification_id}
      data-new={!read}
    >
      <div className="min-w-0 space-y-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">{notification.title}</span>
          {!read ? (
            <Badge variant="secondary" className="h-5 shrink-0 px-1.5 text-[10px]">
              New
            </Badge>
          ) : null}
        </div>
        <p className="max-h-10 overflow-hidden text-xs leading-5 text-muted-foreground">
          {notification.body}
        </p>
        <ElapsedTime
          className="block text-[11px] text-muted-foreground tabular-nums"
          data-testid="notification-created-at"
          dateTime={notification.created_at}
          title={formatDateTimeUTC(notification.created_at)}
          value={notification.created_at}
        />
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label="Dismiss notification"
        title="Dismiss notification"
        onClick={onDismiss}
      >
        <X />
      </Button>
    </article>
  );
}

function numericSequence(value: string | undefined): number {
  if (!value) return 0;
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 0) return 0;
  return parsed;
}
