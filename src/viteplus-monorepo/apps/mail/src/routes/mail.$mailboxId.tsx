import { createFileRoute, getRouteApi, Link, Outlet } from "@tanstack/react-router";
import { useLiveQuery } from "@tanstack/react-db";
import { useMemo } from "react";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { formatUTCDateTime } from "@forge-metal/web-env";
import {
  createEmailMailboxCollection,
  createEmailCollection,
  type ElectricEmail,
} from "~/lib/collections";

export const Route = createFileRoute("/mail/$mailboxId")({
  ssr: "data-only",
  component: MailboxEmailList,
});

const mailRoute = getRouteApi("/mail");

function MailboxEmailList() {
  const { mailboxId } = Route.useParams();
  const result = mailRoute.useLoaderData();

  if (result.status !== "ok") return null;

  return <EmailListPane accountId={result.account.account_id} mailboxId={mailboxId} />;
}

function EmailListPane({ accountId, mailboxId }: { accountId: string; mailboxId: string }) {
  const auth = useSignedInAuth();
  const emailMailboxCollection = useMemo(
    () => createEmailMailboxCollection(auth, accountId),
    [accountId, auth.cachePartition],
  );

  const emailCollection = useMemo(
    () => createEmailCollection(auth, accountId),
    [accountId, auth.cachePartition],
  );

  const { data: emailMailboxes } = useLiveQuery(
    (q) => q.from({ em: emailMailboxCollection }),
    [emailMailboxCollection],
  );

  const { data: allEmails } = useLiveQuery(
    (q) => q.from({ e: emailCollection }),
    [emailCollection],
  );

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
      <div className="w-[360px] shrink-0 border-r border-border overflow-y-auto bg-card">
        {emails.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-muted-foreground px-4">
            <svg
              viewBox="0 0 24 24"
              className="w-12 h-12 mb-3 opacity-40"
              fill="none"
              stroke="currentColor"
              strokeWidth="1"
            >
              <path d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
            </svg>
            <p className="text-sm">No emails in this folder</p>
          </div>
        )}
        <div className="divide-y divide-border">
          {emails.map((email) => (
            <EmailRow key={email.id} email={email} mailboxId={mailboxId} />
          ))}
        </div>
      </div>
      <div className="flex-1 overflow-y-auto bg-background">
        <Outlet />
      </div>
    </div>
  );
}

function EmailRow({ email, mailboxId }: { email: ElectricEmail; mailboxId: string }) {
  const isUnread = !email.is_seen;
  const senderName = email.from_name || email.from_email || "Unknown";
  const initial = senderName[0]?.toUpperCase() ?? "?";

  return (
    <Link
      to="/mail/$mailboxId/$emailId"
      params={{ mailboxId, emailId: email.id }}
      className={`
        group block px-3 py-2.5 hover:bg-accent transition-colors cursor-pointer
        [&.active]:bg-primary/5 [&.active]:border-l-2 [&.active]:border-l-primary [&.active]:pl-[10px]
        ${isUnread ? "bg-card" : "bg-background"}
      `}
    >
      <div className="flex items-start gap-3">
        {/* Sender avatar */}
        <div
          className={`
          w-8 h-8 rounded-full flex items-center justify-center text-xs font-medium shrink-0 mt-0.5
          ${isUnread ? "bg-primary/10 text-primary" : "bg-muted text-muted-foreground"}
        `}
        >
          {initial}
        </div>

        <div className="flex-1 min-w-0">
          {/* Top row: sender + date */}
          <div className="flex items-baseline gap-2">
            <span
              className={`flex-1 truncate text-sm ${isUnread ? "font-semibold text-foreground" : "text-foreground/80"}`}
            >
              {senderName}
            </span>
            <span className="text-[11px] text-muted-foreground shrink-0 tabular-nums">
              {formatDate(email.received_at)}
            </span>
          </div>

          {/* Subject line */}
          <div
            className={`truncate text-sm mt-0.5 ${isUnread ? "text-foreground" : "text-foreground/70"}`}
          >
            {email.is_flagged && (
              <svg
                viewBox="0 0 20 20"
                className="w-3.5 h-3.5 text-warning inline mr-1 -mt-0.5"
                fill="currentColor"
              >
                <path
                  fillRule="evenodd"
                  d="M10.868 2.884c-.321-.772-1.415-.772-1.736 0l-1.83 4.401-4.753.381c-.833.067-1.171 1.107-.536 1.651l3.62 3.102-1.106 4.637c-.194.813.691 1.456 1.405 1.02L10 15.591l4.069 2.485c.713.436 1.598-.207 1.404-1.02l-1.106-4.637 3.62-3.102c.635-.544.297-1.584-.536-1.65l-4.752-.382-1.831-4.401Z"
                  clipRule="evenodd"
                />
              </svg>
            )}
            {email.subject || "(no subject)"}
          </div>

          {/* Preview */}
          <div className="truncate text-xs text-muted-foreground mt-0.5 leading-relaxed">
            {email.preview}
          </div>
        </div>

        {/* Unread indicator */}
        {isUnread && <div className="w-2 h-2 rounded-full bg-primary shrink-0 mt-2" />}
      </div>
    </Link>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const now = new Date();
  const isToday =
    d.getUTCFullYear() === now.getUTCFullYear() &&
    d.getUTCMonth() === now.getUTCMonth() &&
    d.getUTCDate() === now.getUTCDate();
  if (isToday) {
    return formatUTCDateTime(d, { hour: "numeric", minute: "2-digit" });
  }
  const isThisYear = d.getUTCFullYear() === now.getUTCFullYear();
  if (isThisYear) {
    return formatUTCDateTime(d, { month: "short", day: "numeric" });
  }
  return formatUTCDateTime(d, { month: "short", day: "numeric", year: "2-digit" });
}
