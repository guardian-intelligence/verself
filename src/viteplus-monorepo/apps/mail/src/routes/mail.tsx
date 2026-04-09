import { createFileRoute, Link, Outlet, redirect } from "@tanstack/react-router";
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
  loader: () => getMailAccount(),
  component: MailLayout,
});

// Role-based ordering: inbox first, standard roles next, custom alphabetical, trash/junk last.
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
  const account = Route.useLoaderData();
  if (!account) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        No mailbox account found.
      </div>
    );
  }

  return <MailShell account={account} />;
}

function MailShell({ account }: { account: MailAccount }) {
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

  // Count emails per mailbox from the junction table
  const { data: emailMailboxes } = useLiveQuery(
    (q) => q.from({ em: emailMailboxCollection }),
    [emailMailboxCollection],
  );

  // Get all emails for unread counts
  const { data: emails } = useLiveQuery(
    (q) => q.from({ e: emailCollection }),
    [emailCollection],
  );

  const sortedMailboxes = useMemo(() => {
    if (!mailboxes) return [];
    return [...mailboxes].sort((a, b) => mailboxSortKey(a).localeCompare(mailboxSortKey(b)));
  }, [mailboxes]);

  // Compute unread counts per mailbox using emails + email_mailboxes
  const unreadByMailbox = useMemo(() => {
    const counts: Record<string, number> = {};
    if (!emailMailboxes || !emails) return counts;
    const unseenIds = new Set(
      emails.filter((e) => !e.is_seen).map((e) => e.id),
    );
    for (const em of emailMailboxes) {
      if (unseenIds.has(em.email_id)) {
        counts[em.mailbox_id] = (counts[em.mailbox_id] ?? 0) + 1;
      }
    }
    return counts;
  }, [emailMailboxes, emails]);

  return (
    <div className="flex h-full">
      <aside className="w-56 shrink-0 border-r border-border overflow-y-auto bg-card">
        <nav className="py-2">
          {sortedMailboxes.map((m) => (
            <MailboxLink key={m.id} mailbox={m} unread={unreadByMailbox[m.id] ?? 0} />
          ))}
          {sortedMailboxes.length === 0 && (
            <p className="px-3 py-2 text-sm text-muted-foreground">Syncing mailboxes...</p>
          )}
        </nav>
      </aside>
      <div className="flex-1 overflow-hidden">
        <Outlet />
      </div>
    </div>
  );
}

function MailboxLink({ mailbox, unread }: { mailbox: ElectricMailbox; unread: number }) {
  const icon = mailboxIcon(mailbox.role);
  return (
    <Link
      to="/mail/$mailboxId"
      params={{ mailboxId: mailbox.id }}
      className="flex items-center gap-2 px-3 py-1.5 text-sm hover:bg-accent [&.active]:bg-accent [&.active]:text-accent-foreground"
    >
      <span className="w-4 text-center text-muted-foreground">{icon}</span>
      <span className="flex-1 truncate">{mailbox.name}</span>
      {unread > 0 && (
        <span className="text-xs font-medium text-primary">{unread}</span>
      )}
    </Link>
  );
}

function mailboxIcon(role: string): string {
  switch (role) {
    case "inbox":
      return "\u2709";
    case "sent":
      return "\u2191";
    case "drafts":
      return "\u270E";
    case "trash":
      return "\u2717";
    case "junk":
      return "\u26A0";
    case "archive":
      return "\u2610";
    default:
      return "\u2500";
  }
}
