import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Bell, Check, CheckCheck, Inbox, Loader2, Send, Trash2, X } from "lucide-react";
import { useSignedInAuth } from "@verself/auth-web/react";
import { ElapsedTime } from "@verself/ui/components/elapsed-time";
import { Badge } from "@verself/ui/components/ui/badge";
import { Button } from "@verself/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@verself/ui/components/ui/popover";
import { Skeleton } from "@verself/ui/components/ui/skeleton";
import { cn } from "@verself/ui";
import {
  Page,
  PageActions,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
} from "@verself/ui/components/ui/page";
import { ErrorCallout } from "~/components/error-callout";
import { formatDateTimeUTC } from "~/lib/format";
import type { Notification } from "~/server-fns/api";
import { useNotificationInboxState } from "./live";
import {
  useClearNotificationsMutation,
  useDismissNotificationMutation,
  useMarkSingleNotificationReadMutation,
  useMarkNotificationReadMutation,
  usePublishTestNotificationMutation,
} from "./mutations";
import { notificationsQuery } from "./queries";

type NotificationsFilter = "unread" | "all";
type InboxMode = "popover" | "page";

export function NotificationBell() {
  const inbox = useNotificationInbox({ limit: 10 });
  const unreadBadgeLabel = inbox.unreadCount > 99 ? "!" : String(inbox.unreadCount);

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
            {inbox.unreadCount > 0 ? (
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
              {inbox.unreadCount > 0 ? `${inbox.unreadCount} unread` : "No unread"}
            </div>
          </div>
          <NotificationActions inbox={inbox} mode="popover" />
        </PopoverHeader>
        <NotificationInboxList inbox={inbox} mode="popover" />
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

export function NotificationsPage() {
  const inbox = useNotificationInbox({ limit: 100 });

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Notifications</PageTitle>
          <PageDescription>
            {inbox.unreadCount > 0 ? `${inbox.unreadCount} unread` : "No unread notifications"}
          </PageDescription>
        </PageHeaderContent>
        <PageActions>
          <NotificationActions inbox={inbox} mode="page" />
        </PageActions>
      </PageHeader>
      <PageSections>
        <PageSection>
          <NotificationInboxList inbox={inbox} mode="page" />
        </PageSection>
      </PageSections>
    </Page>
  );
}

export function NotificationsPageFallback() {
  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Notifications</PageTitle>
          <PageDescription>Loading notifications</PageDescription>
        </PageHeaderContent>
      </PageHeader>
      <PageSections>
        <PageSection>
          <LoadingRows />
        </PageSection>
      </PageSections>
    </Page>
  );
}

function useNotificationInbox({ limit }: { readonly limit: number }) {
  const auth = useSignedInAuth();
  const live = useNotificationInboxState();
  const query = useQuery(
    notificationsQuery(auth, {
      latestSequence: live.latestSequence,
      limit,
      readUpToSequence: live.readUpToSequence,
      revision: live.revision,
    }),
  );
  const summary = query.data?.summary;
  const hasLiveState = live.inboxState !== null;
  const summaryUnreadCount = summary?.unread_count ?? 0;
  const latestSequence = hasLiveState
    ? live.latestSequence
    : numericSequence(summary?.latest_sequence);
  const readUpToSequence = hasLiveState
    ? live.readUpToSequence
    : numericSequence(summary?.read_up_to_sequence);
  const fallbackUnreadCount = Math.max(summaryUnreadCount, latestSequence - readUpToSequence);
  const currentSummaryUnreadCount = query.isPlaceholderData ? null : summaryUnreadCount;
  const unreadCount = Math.min(
    Math.max(
      currentSummaryUnreadCount ?? (hasLiveState ? live.unreadCount : fallbackUnreadCount),
      0,
    ),
    999,
  );
  const notifications = query.data?.notifications ?? [];

  return {
    busy: query.isFetching,
    latestSequence,
    notifications,
    query,
    readUpToSequence,
    unreadCount,
  };
}

function NotificationActions({
  inbox,
  mode,
}: {
  readonly inbox: ReturnType<typeof useNotificationInbox>;
  readonly mode: InboxMode;
}) {
  const markRead = useMarkNotificationReadMutation();
  const clear = useClearNotificationsMutation();
  const test = usePublishTestNotificationMutation();
  const busy = inbox.busy || markRead.isPending || clear.isPending || test.isPending;

  return (
    <div className="flex shrink-0 items-center gap-1">
      {busy ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : null}
      {mode === "popover" ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="Open notifications"
          title="Open notifications"
          render={<Link to="/notifications" />}
        >
          <Inbox />
        </Button>
      ) : null}
      <Button
        type="button"
        variant={mode === "page" ? "outline" : "ghost"}
        size={mode === "page" ? "sm" : "icon-xs"}
        aria-label="Send test notification"
        title="Send test notification"
        data-testid="notifications-test"
        onClick={() => test.mutate({ title: "Notification test" })}
      >
        <Send />
        {mode === "page" ? <span>Test</span> : null}
      </Button>
      <Button
        type="button"
        variant={mode === "page" ? "outline" : "ghost"}
        size={mode === "page" ? "sm" : "icon-xs"}
        aria-label="Mark notifications read"
        title="Mark notifications read"
        data-testid="notifications-mark-read"
        aria-busy={markRead.isPending}
        onClick={() => {
          if (inbox.latestSequence <= inbox.readUpToSequence) return;
          markRead.mutate({ read_up_to_sequence: String(Math.max(inbox.latestSequence, 0)) });
        }}
      >
        <CheckCheck />
        {mode === "page" ? <span>Mark read</span> : null}
      </Button>
      {mode === "page" ? (
        <Button
          type="button"
          variant="destructive"
          size="sm"
          aria-label="Clear notifications"
          data-testid="notifications-clear-all"
          aria-busy={clear.isPending}
          onClick={() => clear.mutate()}
        >
          <Trash2 />
          <span>Clear all</span>
        </Button>
      ) : null}
    </div>
  );
}

function NotificationInboxList({
  inbox,
  mode,
}: {
  readonly inbox: ReturnType<typeof useNotificationInbox>;
  readonly mode: InboxMode;
}) {
  const [filter, setFilter] = useState<NotificationsFilter>("unread");
  const markRead = useMarkSingleNotificationReadMutation();
  const dismiss = useDismissNotificationMutation();
  const visibleNotifications =
    filter === "unread"
      ? inbox.notifications.filter(
          (notification) => !isNotificationRead(notification, inbox.readUpToSequence),
        )
      : inbox.notifications;
  const emptyMessage = filter === "unread" ? "No unread notifications" : "No notifications";

  return (
    <div className={cn(mode === "popover" ? "max-h-96 overflow-y-auto" : "min-h-56")}>
      <div className="flex items-center justify-between gap-3 border-b px-3 py-2">
        <FilterToggle value={filter} onChange={setFilter} />
      </div>
      {inbox.query.isError ? (
        <div className="p-3" data-testid="notifications-error">
          <ErrorCallout title="Notifications unavailable" error={inbox.query.error} />
        </div>
      ) : inbox.query.isPending ? (
        <LoadingRows />
      ) : visibleNotifications.length === 0 ? (
        <div
          className="px-3 py-8 text-center text-sm text-muted-foreground"
          data-testid="notifications-empty"
        >
          {emptyMessage}
        </div>
      ) : (
        <div className="divide-y">
          {visibleNotifications.map((notification) => {
            const read = isNotificationRead(notification, inbox.readUpToSequence);
            return (
              <NotificationRow
                key={notification.notification_id}
                notification={notification}
                read={read}
                onMarkRead={() => {
                  if (read || markRead.isPending) return;
                  markRead.mutate({ notification_id: notification.notification_id });
                }}
                onDismiss={() => dismiss.mutate({ notification_id: notification.notification_id })}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

function FilterToggle({
  onChange,
  value,
}: {
  readonly onChange: (value: NotificationsFilter) => void;
  readonly value: NotificationsFilter;
}) {
  return (
    <div
      role="group"
      aria-label="Notification filter"
      className="inline-flex rounded-md border border-border bg-background p-0.5"
    >
      {(["unread", "all"] as const).map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={value === option}
          data-testid={`notifications-filter-${option}`}
          onClick={() => onChange(option)}
          className={cn(
            "h-7 rounded-sm px-2.5 text-xs font-medium transition-colors",
            value === option
              ? "bg-accent text-accent-foreground"
              : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          {option === "unread" ? "Unread" : "All"}
        </button>
      ))}
    </div>
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
  onDismiss,
  onMarkRead,
  read,
}: {
  readonly notification: Notification;
  readonly onDismiss: () => void;
  readonly onMarkRead: () => void;
  readonly read: boolean;
}) {
  return (
    <article
      className={cn(
        "grid grid-cols-[1fr_auto] gap-2 px-3 py-2.5 transition-colors duration-200 data-[new=true]:animate-[notification-flash_1.1s_ease-out]",
        !read && "bg-muted/35",
      )}
      data-testid="notification-row"
      data-notification-id={notification.notification_id}
      data-new={!read}
    >
      <div className="min-w-0 space-y-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">{notification.title}</span>
          {!read ? (
            <Badge
              variant="secondary"
              className="h-5 shrink-0 border border-border bg-background px-1.5 text-[10px] font-medium text-muted-foreground"
            >
              Unread
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
      <div className="flex items-start gap-1">
        {!read ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label="Mark notification read"
            title="Mark notification read"
            data-testid="notification-mark-read"
            onClick={(event) => {
              event.stopPropagation();
              onMarkRead();
            }}
          >
            <Check />
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="Dismiss notification"
          title="Dismiss notification"
          onClick={(event) => {
            event.stopPropagation();
            onDismiss();
          }}
        >
          <X />
        </Button>
      </div>
    </article>
  );
}

function isNotificationRead(notification: Notification, readUpToSequence: number): boolean {
  return (
    notification.read_at !== undefined ||
    numericSequence(notification.recipient_sequence) <= readUpToSequence
  );
}

function numericSequence(value: string | undefined): number {
  if (!value) return 0;
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 0) return 0;
  return parsed;
}
