import { getAccessToken } from "./auth";

async function authFetch(path: string, init?: RequestInit): Promise<Response> {
  const token = await getAccessToken();
  const headers = new Headers(init?.headers);
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  headers.set("Accept", "application/json");
  return fetch(path, { ...init, headers });
}

async function jsonOrThrow<T>(resp: Response): Promise<T> {
  if (!resp.ok) {
    const body = await resp.text().catch(() => "");
    throw new Error(`API ${resp.status}: ${body}`);
  }
  return resp.json();
}

// --- Account ---

export interface MailAccount {
  account_id: string;
  email_address: string;
  display_name: string;
}

export function fetchAccount(): Promise<MailAccount> {
  return authFetch("/api/v1/mail/account").then(jsonOrThrow<MailAccount>);
}

// --- Email mutations ---

export function markRead(emailId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/read`, { method: "POST" }).then(
    jsonOrThrow,
  ) as Promise<void>;
}

export function markUnread(emailId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/unread`, { method: "POST" }).then(
    jsonOrThrow,
  ) as Promise<void>;
}

export function flagEmail(emailId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/flag`, { method: "POST" }).then(
    jsonOrThrow,
  ) as Promise<void>;
}

export function unflagEmail(emailId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/unflag`, { method: "POST" }).then(
    jsonOrThrow,
  ) as Promise<void>;
}

export function moveEmail(emailId: string, mailboxId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/move`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mailbox_id: mailboxId }),
  }).then(jsonOrThrow) as Promise<void>;
}

export function trashEmail(emailId: string): Promise<void> {
  return authFetch(`/api/v1/mail/emails/${emailId}/trash`, { method: "POST" }).then(
    jsonOrThrow,
  ) as Promise<void>;
}

// --- Email body ---

export interface EmailBody {
  account_id: string;
  email_id: string;
  text_body: string;
  html_body: string;
  fetched_at: string;
}

export function fetchEmailBody(emailId: string): Promise<EmailBody> {
  return authFetch(`/api/v1/mail/emails/${emailId}/body`).then(jsonOrThrow<EmailBody>);
}

// --- Sync status ---

export function fetchSyncStatus(): Promise<{ status: unknown }> {
  return authFetch("/api/v1/mail/sync/status").then(jsonOrThrow<{ status: unknown }>);
}
