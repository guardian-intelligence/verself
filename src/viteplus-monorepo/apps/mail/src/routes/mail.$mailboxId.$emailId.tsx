import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import DOMPurify from "dompurify";
import {
  flagEmail,
  getEmailBody,
  markEmailRead,
  unflagEmail,
  trashEmail,
  type EmailBody,
} from "~/server-fns/mail";
import { useLiveQuery } from "@tanstack/react-db";
import { createEmailCollection, type ElectricEmail } from "~/lib/collections";
import { keys } from "~/lib/query-keys";

export const Route = createFileRoute("/mail/$mailboxId/$emailId")({
  ssr: "data-only",
  component: EmailViewer,
});

const mailRoute = getRouteApi("/mail");

function EmailViewer() {
  const { emailId } = Route.useParams();
  const account = mailRoute.useLoaderData();

  if (!account) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Loading...
      </div>
    );
  }

  return <EmailViewerInner accountId={account.account_id} emailId={emailId} />;
}

function EmailViewerInner({ accountId, emailId }: { accountId: string; emailId: string }) {
  // Fetch the email body via API (triggers JMAP fetch + PG cache on first load)
  const { data: body, isLoading: bodyLoading } = useQuery({
    queryKey: keys.emailBody(emailId),
    queryFn: () => getEmailBody({ data: { emailId } }),
    staleTime: 5 * 60_000,
  });

  // Get the email metadata from Electric for real-time flag/seen state
  const emailCollection = useMemo(
    () => createEmailCollection(accountId),
    [accountId],
  );

  const { data: emails } = useLiveQuery(
    (q) => q.from({ e: emailCollection }),
    [emailCollection],
  );

  const email = useMemo(
    () => (emails as ElectricEmail[] | undefined)?.find((e) => e.id === emailId) ?? null,
    [emails, emailId],
  );

  // Auto-mark as read when the email is opened
  const markedReadRef = useRef<string | null>(null);
  useEffect(() => {
    if (email && !email.is_seen && markedReadRef.current !== emailId) {
      markedReadRef.current = emailId;
      void markEmailRead({ data: { emailId } });
    }
  }, [email, emailId]);

  if (!email) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Select an email
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <EmailHeader email={email} />
      <div className="flex-1 overflow-y-auto p-4">
        {bodyLoading ? (
          <p className="text-muted-foreground">Loading...</p>
        ) : body ? (
          <EmailBodyRenderer body={body} />
        ) : (
          <p className="text-muted-foreground">No content</p>
        )}
      </div>
    </div>
  );
}

function EmailHeader({ email }: { email: ElectricEmail }) {
  const handleFlag = () => {
    void (
      email.is_flagged
        ? unflagEmail({ data: { emailId: email.id } })
        : flagEmail({ data: { emailId: email.id } })
    );
  };

  const handleTrash = () => {
    void trashEmail({ data: { emailId: email.id } });
  };

  const toList = safeParseJson<Array<{ name?: string; email: string }>>(email.to_list);

  return (
    <div className="border-b border-border p-4 shrink-0">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <h2 className="text-lg font-semibold truncate">{email.subject || "(no subject)"}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            From: <span className="text-foreground">{email.from_name || email.from_email}</span>
            {email.from_name && (
              <span className="ml-1">&lt;{email.from_email}&gt;</span>
            )}
          </p>
          {toList.length > 0 && (
            <p className="text-sm text-muted-foreground">
              To:{" "}
              {toList.map((r) => r.name || r.email).join(", ")}
            </p>
          )}
          <p className="text-xs text-muted-foreground mt-1">
            {new Date(email.received_at).toLocaleString()}
          </p>
        </div>
        <div className="flex gap-1 shrink-0">
          <button
            onClick={handleFlag}
            className={`px-2 py-1 rounded text-sm border border-border hover:bg-accent ${email.is_flagged ? "text-warning" : "text-muted-foreground"}`}
            title={email.is_flagged ? "Unflag" : "Flag"}
          >
            {email.is_flagged ? "\u2605" : "\u2606"}
          </button>
          <button
            onClick={handleTrash}
            className="px-2 py-1 rounded text-sm border border-border hover:bg-accent text-muted-foreground hover:text-destructive"
            title="Trash"
          >
            {"\u2717"}
          </button>
        </div>
      </div>
    </div>
  );
}

function EmailBodyRenderer({ body }: { body: EmailBody }) {
  if (body.html_body) {
    const clean = DOMPurify.sanitize(body.html_body, {
      USE_PROFILES: { html: true },
      ADD_TAGS: ["style"],
      FORBID_TAGS: ["script", "iframe", "object", "embed", "form"],
      FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover"],
    });
    return (
      <div
        className="prose prose-sm max-w-none"
        dangerouslySetInnerHTML={{ __html: clean }}
      />
    );
  }

  if (body.text_body) {
    return <pre className="whitespace-pre-wrap text-sm font-mono">{body.text_body}</pre>;
  }

  return <p className="text-muted-foreground">Empty message</p>;
}

function safeParseJson<T>(raw: string): T {
  try {
    return JSON.parse(raw) as T;
  } catch {
    return [] as unknown as T;
  }
}
