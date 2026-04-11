import { createFileRoute, getRouteApi, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import DOMPurify from "dompurify";
import { authQueryKey } from "@forge-metal/auth-web/isomorphic";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { formatUTCDateTime } from "@forge-metal/web-env";
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

export const Route = createFileRoute("/mail/$mailboxId/$emailId")({
  ssr: "data-only",
  component: EmailViewer,
});

const mailRoute = getRouteApi("/mail");

function EmailViewer() {
  const { emailId } = Route.useParams();
  const result = mailRoute.useLoaderData();

  if (result.status !== "ok") {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Loading...
      </div>
    );
  }

  return <EmailViewerInner accountId={result.account.account_id} emailId={emailId} />;
}

function EmailViewerInner({ accountId, emailId }: { accountId: string; emailId: string }) {
  const auth = useSignedInAuth();
  const { mailboxId } = Route.useParams();
  const navigate = useNavigate();

  const { data: body, isLoading: bodyLoading } = useQuery({
    queryKey: authQueryKey(auth, "mail", "body", emailId),
    queryFn: () => getEmailBody({ data: { emailId } }),
    staleTime: 5 * 60_000,
  });

  const emailCollection = useMemo(
    () => createEmailCollection(auth, accountId),
    [accountId, auth.cachePartition],
  );

  const { data: emails } = useLiveQuery((q) => q.from({ e: emailCollection }), [emailCollection]);

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
        <div className="text-center space-y-2">
          <svg
            viewBox="0 0 24 24"
            className="w-10 h-10 mx-auto opacity-30"
            fill="none"
            stroke="currentColor"
            strokeWidth="1"
          >
            <path d="M21.75 6.75v10.5a2.25 2.25 0 0 1-2.25 2.25h-15a2.25 2.25 0 0 1-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25m19.5 0v.243a2.25 2.25 0 0 1-1.07 1.916l-7.5 4.615a2.25 2.25 0 0 1-2.36 0L3.32 8.91a2.25 2.25 0 0 1-1.07-1.916V6.75" />
          </svg>
          <p className="text-sm">Select an email to read</p>
        </div>
      </div>
    );
  }

  const handleFlag = () => {
    void (email.is_flagged
      ? unflagEmail({ data: { emailId: email.id } })
      : flagEmail({ data: { emailId: email.id } }));
  };

  const handleTrash = () => {
    void trashEmail({ data: { emailId: email.id } });
    void navigate({ to: "/mail/$mailboxId", params: { mailboxId } });
  };

  return (
    <div className="flex flex-col h-full">
      <EmailToolbar email={email} onFlag={handleFlag} onTrash={handleTrash} />
      <EmailHeader email={email} />
      <div className="flex-1 overflow-y-auto">
        <div className="px-6 py-4 max-w-3xl">
          {bodyLoading ? (
            <div className="space-y-3 animate-pulse">
              <div className="h-4 bg-muted rounded w-full" />
              <div className="h-4 bg-muted rounded w-5/6" />
              <div className="h-4 bg-muted rounded w-4/6" />
              <div className="h-4 bg-muted rounded w-full" />
              <div className="h-4 bg-muted rounded w-3/6" />
            </div>
          ) : body ? (
            <EmailBodyRenderer body={body} />
          ) : (
            <p className="text-muted-foreground text-sm">No content</p>
          )}
        </div>
      </div>
    </div>
  );
}

function EmailToolbar({
  email,
  onFlag,
  onTrash,
}: {
  email: ElectricEmail;
  onFlag: () => void;
  onTrash: () => void;
}) {
  return (
    <div className="flex items-center gap-1 px-4 py-2 border-b border-border bg-card shrink-0">
      <ToolbarButton
        onClick={onFlag}
        title={email.is_flagged ? "Remove star" : "Star"}
        active={email.is_flagged}
      >
        {email.is_flagged ? (
          <svg viewBox="0 0 20 20" className="w-4 h-4 text-warning" fill="currentColor">
            <path
              fillRule="evenodd"
              d="M10.868 2.884c-.321-.772-1.415-.772-1.736 0l-1.83 4.401-4.753.381c-.833.067-1.171 1.107-.536 1.651l3.62 3.102-1.106 4.637c-.194.813.691 1.456 1.405 1.02L10 15.591l4.069 2.485c.713.436 1.598-.207 1.404-1.02l-1.106-4.637 3.62-3.102c.635-.544.297-1.584-.536-1.65l-4.752-.382-1.831-4.401Z"
              clipRule="evenodd"
            />
          </svg>
        ) : (
          <svg
            viewBox="0 0 24 24"
            className="w-4 h-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z" />
          </svg>
        )}
      </ToolbarButton>

      <ToolbarButton onClick={onTrash} title="Move to Trash">
        <svg
          viewBox="0 0 24 24"
          className="w-4 h-4"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" />
        </svg>
      </ToolbarButton>

      <div className="flex-1" />

      <span className="text-xs text-muted-foreground">
        {formatUTCDateTime(email.received_at, {
          weekday: "short",
          month: "short",
          day: "numeric",
          year: "numeric",
          hour: "numeric",
          minute: "2-digit",
          timeZoneName: "short",
        })}
      </span>
    </div>
  );
}

function ToolbarButton({
  onClick,
  title,
  active,
  children,
}: {
  onClick: () => void;
  title: string;
  active?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={`
        p-2 rounded-lg transition-colors
        ${active ? "text-warning bg-warning/10" : "text-muted-foreground hover:text-foreground hover:bg-accent"}
      `}
    >
      {children}
    </button>
  );
}

function EmailHeader({ email }: { email: ElectricEmail }) {
  const toList = safeParseJson(email.to_list, [] as Array<{ name?: string; email: string }>);
  const ccList = safeParseJson(email.cc_list, [] as Array<{ name?: string; email: string }>);
  const senderName = email.from_name || email.from_email;
  const senderInitial = senderName?.[0]?.toUpperCase() ?? "?";

  return (
    <div className="border-b border-border px-6 py-4 bg-card shrink-0">
      <h2 className="text-xl font-semibold leading-tight mb-4">
        {email.subject || "(no subject)"}
      </h2>

      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-full bg-primary/10 text-primary flex items-center justify-center text-sm font-medium shrink-0">
          {senderInitial}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="font-medium text-sm">{email.from_name || email.from_email}</span>
            {email.from_name && (
              <span className="text-xs text-muted-foreground">&lt;{email.from_email}&gt;</span>
            )}
          </div>
          <div className="text-xs text-muted-foreground mt-0.5 space-y-0.5">
            {toList.length > 0 && <p>to {toList.map((r) => r.name || r.email).join(", ")}</p>}
            {ccList.length > 0 && <p>cc {ccList.map((r) => r.name || r.email).join(", ")}</p>}
          </div>
        </div>
      </div>
    </div>
  );
}

function EmailBodyRenderer({ body }: { body: EmailBody }) {
  if (body.html_body) {
    const clean = DOMPurify.sanitize(body.html_body, {
      USE_PROFILES: { html: true },
      ALLOWED_TAGS: [
        "a",
        "abbr",
        "address",
        "article",
        "b",
        "bdi",
        "bdo",
        "blockquote",
        "br",
        "caption",
        "center",
        "cite",
        "code",
        "col",
        "colgroup",
        "dd",
        "del",
        "details",
        "dfn",
        "div",
        "dl",
        "dt",
        "em",
        "figcaption",
        "figure",
        "font",
        "footer",
        "h1",
        "h2",
        "h3",
        "h4",
        "h5",
        "h6",
        "header",
        "hr",
        "i",
        "img",
        "ins",
        "kbd",
        "li",
        "main",
        "mark",
        "nav",
        "ol",
        "p",
        "pre",
        "q",
        "rp",
        "rt",
        "ruby",
        "s",
        "samp",
        "section",
        "small",
        "span",
        "strong",
        "style",
        "sub",
        "summary",
        "sup",
        "table",
        "tbody",
        "td",
        "tfoot",
        "th",
        "thead",
        "time",
        "tr",
        "u",
        "ul",
        "var",
        "wbr",
      ],
      ALLOWED_ATTR: [
        "align",
        "alt",
        "border",
        "cellpadding",
        "cellspacing",
        "class",
        "color",
        "colspan",
        "dir",
        "face",
        "height",
        "href",
        "hspace",
        "lang",
        "rowspan",
        "size",
        "src",
        "style",
        "summary",
        "target",
        "title",
        "valign",
        "vspace",
        "width",
      ],
      ALLOW_DATA_ATTR: false,
    });
    return (
      <div
        className="email-body-frame text-sm leading-relaxed"
        dangerouslySetInnerHTML={{ __html: clean }}
      />
    );
  }

  if (body.text_body) {
    return (
      <pre className="whitespace-pre-wrap text-sm font-mono leading-relaxed text-foreground/90">
        {body.text_body}
      </pre>
    );
  }

  return <p className="text-muted-foreground text-sm">Empty message</p>;
}

function safeParseJson<T>(raw: string, fallback: T): T {
  try {
    const parsed = JSON.parse(raw) as T | null;
    return parsed ?? fallback;
  } catch {
    return fallback;
  }
}
