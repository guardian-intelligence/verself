import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import { fetchAccount } from "~/lib/api";
import {
  createEmailMailboxCollection,
  createEmailCollection,
  type ElectricEmail,
} from "~/lib/collections";
import { keys } from "~/lib/query-keys";

export const Route = createFileRoute("/mail/$mailboxId")({
  component: MailboxEmailList,
});

function MailboxEmailList() {
  const { mailboxId } = Route.useParams();
  const { data: account } = useQuery({
    queryKey: keys.account(),
    queryFn: fetchAccount,
    staleTime: Infinity,
  });

  if (!account) return null;

  return <EmailListPane accountId={account.account_id} mailboxId={mailboxId} />;
}

function EmailListPane({ accountId, mailboxId }: { accountId: string; mailboxId: string }) {
  const emailMailboxCollection = useMemo(
    () => createEmailMailboxCollection(accountId),
    [accountId],
  );

  const emailCollection = useMemo(
    () => createEmailCollection(accountId),
    [accountId],
  );

  const { data: emailMailboxes } = useLiveQuery(
    (q) => q.from({ em: emailMailboxCollection }),
    [emailMailboxCollection],
  );

  const { data: allEmails } = useLiveQuery(
    (q) => q.from({ e: emailCollection }),
    [emailCollection],
  );

  // Filter emails that belong to this mailbox and sort by received_at desc
  const emails = useMemo(() => {
    if (!emailMailboxes || !allEmails) return [];
    const emailIdsInMailbox = new Set(
      emailMailboxes.filter((em) => em.mailbox_id === mailboxId).map((em) => em.email_id),
    );
    return allEmails
      .filter((e) => emailIdsInMailbox.has(e.id))
      .sort((a, b) => b.received_at.localeCompare(a.received_at));
  }, [emailMailboxes, allEmails, mailboxId]);

  return (
    <div className="flex h-full">
      <div className="w-80 shrink-0 border-r border-border overflow-y-auto">
        {emails.map((email) => (
          <EmailRow key={email.id} email={email} mailboxId={mailboxId} />
        ))}
        {emails.length === 0 && (
          <p className="px-4 py-8 text-sm text-muted-foreground text-center">No emails</p>
        )}
      </div>
      <div className="flex-1 overflow-y-auto">
        <Outlet />
      </div>
    </div>
  );
}

function EmailRow({ email, mailboxId }: { email: ElectricEmail; mailboxId: string }) {
  const isUnread = !email.is_seen;
  return (
    <Link
      to="/mail/$mailboxId/$emailId"
      params={{ mailboxId, emailId: email.id }}
      className={`block px-3 py-2.5 border-b border-border hover:bg-accent [&.active]:bg-accent text-sm ${isUnread ? "font-semibold" : ""}`}
    >
      <div className="flex items-center gap-2">
        <span className="truncate flex-1">
          {email.from_name || email.from_email || "Unknown"}
        </span>
        <span className="text-xs text-muted-foreground shrink-0">
          {formatDate(email.received_at)}
        </span>
      </div>
      <div className="truncate mt-0.5">
        {email.is_flagged && <span className="text-warning mr-1">{"\u2605"}</span>}
        {email.subject || "(no subject)"}
      </div>
      <div className="truncate text-xs text-muted-foreground mt-0.5">{email.preview}</div>
    </Link>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  const now = new Date();
  const isToday =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (isToday) {
    return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  }
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
