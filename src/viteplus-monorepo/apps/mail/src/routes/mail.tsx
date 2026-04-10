import {
  createFileRoute,
  Link,
  Navigate,
  Outlet,
  redirect,
  useLocation,
} from "@tanstack/react-router";
import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import {
  createMailboxCollection,
  createEmailMailboxCollection,
  createEmailCollection,
  type ElectricMailbox,
} from "~/lib/collections";
import { getViewer } from "~/server-fns/auth";
import { getMailAccount, type MailAccount } from "~/server-fns/mail";

export const Route = createFileRoute("/mail")({
  ssr: "data-only",
  beforeLoad: async ({ location }) => {
    const viewer = await getViewer();
    if (!viewer) {
      throw redirect({
        to: "/login",
        search: { redirect: location.href },
      });
    }
  },
  loader: async ({ location }) => {
    const result = await getMailAccount();
    if (
      location.pathname === "/mail" &&
      result.status === "ok" &&
      result.account.default_mailbox_id
    ) {
      throw redirect({
        to: "/mail/$mailboxId",
        params: { mailboxId: result.account.default_mailbox_id },
        replace: true,
      });
    }
    return result;
  },
  component: MailLayout,
});

const ROLE_ORDER: Record<string, number> = {
  inbox: 0,
  drafts: 1,
  sent: 2,
  archive: 3,
  trash: 100,
  junk: 101,
};

function mailboxSortKey(m: ElectricMailbox): string {
  const order = ROLE_ORDER[m.role] ?? 50;
  return `${String(order).padStart(3, "0")}-${m.name.toLowerCase()}`;
}

function MailLayout() {
  const result = Route.useLoaderData();

  if (result.status === "no_binding") {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center space-y-3 max-w-sm px-4">
          <div className="mx-auto w-12 h-12 rounded-full bg-muted flex items-center justify-center">
            <svg
              viewBox="0 0 24 24"
              className="w-6 h-6 text-muted-foreground"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
            >
              <path d="M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636" />
            </svg>
          </div>
          <p className="text-lg font-medium">No mailbox linked</p>
          <p className="text-sm text-muted-foreground">
            Your account is authenticated but not bound to a mailbox. Ask your administrator to
            create a mailbox binding for your identity.
          </p>
        </div>
      </div>
    );
  }

  if (result.status === "not_found") {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        No mailbox account found.
      </div>
    );
  }

  return <MailShell account={result.account} />;
}

function MailShell({ account }: { account: MailAccount }) {
  const location = useLocation();
  const isMailboxRoot = location.pathname === "/mail";
  const mailboxCollection = useMemo(
    () => createMailboxCollection(account.account_id),
    [account.account_id],
  );

  const emailMailboxCollection = useMemo(
    () => createEmailMailboxCollection(account.account_id),
    [account.account_id],
  );

  const emailCollection = useMemo(
    () => createEmailCollection(account.account_id),
    [account.account_id],
  );

  const { data: mailboxes } = useLiveQuery(
    (q) => q.from({ m: mailboxCollection }),
    [mailboxCollection],
  );

  const { data: emailMailboxes } = useLiveQuery(
    (q) => q.from({ em: emailMailboxCollection }),
    [emailMailboxCollection],
  );

  const { data: emails } = useLiveQuery((q) => q.from({ e: emailCollection }), [emailCollection]);

  const sortedMailboxes = useMemo(() => {
    if (!mailboxes) return [];
    return [...mailboxes].sort((a, b) => mailboxSortKey(a).localeCompare(mailboxSortKey(b)));
  }, [mailboxes]);

  const unreadByMailbox = useMemo(() => {
    const counts: Record<string, number> = {};
    if (!emailMailboxes || !emails) return counts;
    const unseenIds = new Set(emails.filter((e) => !e.is_seen).map((e) => e.id));
    for (const em of emailMailboxes) {
      if (unseenIds.has(em.email_id)) {
        counts[em.mailbox_id] = (counts[em.mailbox_id] ?? 0) + 1;
      }
    }
    return counts;
  }, [emailMailboxes, emails]);

  const defaultMailboxID = account.default_mailbox_id ?? sortedMailboxes[0]?.id;

  if (isMailboxRoot && defaultMailboxID) {
    return <Navigate to="/mail/$mailboxId" params={{ mailboxId: defaultMailboxID }} replace />;
  }

  return (
    <div className="flex h-full">
      <aside className="w-60 shrink-0 border-r border-border overflow-y-auto bg-sidebar">
        <div className="p-3">
          <div className="mb-1 px-2">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              {account.email_address}
            </p>
          </div>
        </div>
        <nav className="px-2 pb-3 space-y-0.5">
          {sortedMailboxes.map((m) => (
            <MailboxLink key={m.id} mailbox={m} unread={unreadByMailbox[m.id] ?? 0} />
          ))}
          {sortedMailboxes.length === 0 && (
            <div className="px-3 py-6 text-center">
              <div className="animate-pulse space-y-2">
                <div className="h-4 bg-muted rounded w-3/4 mx-auto" />
                <div className="h-4 bg-muted rounded w-1/2 mx-auto" />
                <div className="h-4 bg-muted rounded w-2/3 mx-auto" />
              </div>
              <p className="text-xs text-muted-foreground mt-3">Syncing mailboxes...</p>
            </div>
          )}
        </nav>
      </aside>
      <div className="flex-1 overflow-hidden">
        {isMailboxRoot ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Loading mailbox...
          </div>
        ) : (
          <Outlet />
        )}
      </div>
    </div>
  );
}

function MailboxLink({ mailbox, unread }: { mailbox: ElectricMailbox; unread: number }) {
  const icon = mailboxIcon(mailbox.role);
  const isInbox = mailbox.role === "inbox";

  return (
    <Link
      to="/mail/$mailboxId"
      params={{ mailboxId: mailbox.id }}
      className={`
        group flex items-center gap-2.5 px-3 py-1.5 rounded-lg text-sm transition-colors
        text-sidebar-foreground hover:bg-accent
        [&.active]:bg-sidebar-active [&.active]:text-sidebar-active-foreground [&.active]:font-medium
        ${isInbox && unread > 0 ? "font-medium" : ""}
      `}
    >
      <span className="w-5 h-5 flex items-center justify-center text-muted-foreground group-[&.active]:text-sidebar-active-foreground shrink-0">
        {icon}
      </span>
      <span className="flex-1 truncate">{mailbox.name}</span>
      {unread > 0 && (
        <span
          className={`text-xs tabular-nums ${isInbox ? "font-semibold text-sidebar-active-foreground" : "text-muted-foreground"}`}
        >
          {unread}
        </span>
      )}
    </Link>
  );
}

function mailboxIcon(role: string) {
  const cls = "w-[18px] h-[18px]";
  switch (role) {
    case "inbox":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="M2.25 13.5h3.86a2.25 2.25 0 0 1 2.012 1.244l.256.512a2.25 2.25 0 0 0 2.013 1.244h3.218a2.25 2.25 0 0 0 2.013-1.244l.256-.512a2.25 2.25 0 0 1 2.013-1.244h3.859m-19.5.338V18a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18v-4.162c0-.224-.034-.447-.1-.661L19.24 5.338a2.25 2.25 0 0 0-2.15-1.588H6.911a2.25 2.25 0 0 0-2.15 1.588L2.35 13.177a2.25 2.25 0 0 0-.1.661Z" />
        </svg>
      );
    case "sent":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5" />
        </svg>
      );
    case "drafts":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L10.582 16.07a4.5 4.5 0 0 1-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 0 1 1.13-1.897l8.932-8.931Zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0 1 15.75 21H5.25A2.25 2.25 0 0 1 3 18.75V8.25A2.25 2.25 0 0 1 5.25 6H10" />
        </svg>
      );
    case "trash":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
        </svg>
      );
    case "junk":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z" />
        </svg>
      );
    case "archive":
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m20.25 7.5-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125Z" />
        </svg>
      );
    default:
      return (
        <svg
          viewBox="0 0 24 24"
          className={cls}
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.061-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z" />
        </svg>
      );
  }
}
