import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

// Electric shape URL — proxied through Caddy at the same origin.
// Electric syncs the mailbox_service database. The browser never talks JMAP;
// it reads reactive Postgres rows via Electric shape subscriptions.
const ELECTRIC_SHAPE_URL = "/v1/shape";

// --- Mailbox collection ---

export interface ElectricMailbox {
  account_id: string;
  id: string;
  name: string;
  parent_id: string;
  role: string;
  sort_order: number;
  total_emails: number;
  unread_emails: number;
  total_threads: number;
  unread_threads: number;
  synced_at: string;
}

export function createMailboxCollection(accountId: string) {
  return createCollection<ElectricMailbox>(
    electricCollectionOptions({
      id: `sync-mailboxes-${accountId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "mailboxes",
          where: `account_id = '${accountId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.account_id)}:${String(item.id)}`,
    }) as any,
  );
}

// --- Email collection ---

export interface ElectricEmail {
  account_id: string;
  id: string;
  blob_id: string;
  thread_id: string;
  subject: string;
  from_name: string;
  from_email: string;
  to_list: string;
  cc_list: string;
  reply_to_list: string;
  preview: string;
  keywords: string;
  has_attachment: boolean;
  size: number;
  received_at: string;
  sent_at: string;
  is_seen: boolean;
  is_flagged: boolean;
  is_answered: boolean;
  is_draft: boolean;
  synced_at: string;
}

export function createEmailCollection(accountId: string) {
  return createCollection<ElectricEmail>(
    electricCollectionOptions({
      id: `sync-emails-${accountId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "emails",
          where: `account_id = '${accountId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.account_id)}:${String(item.id)}`,
    }) as any,
  );
}

// --- Email-mailbox junction collection ---

export interface ElectricEmailMailbox {
  account_id: string;
  email_id: string;
  mailbox_id: string;
}

export function createEmailMailboxCollection(accountId: string) {
  return createCollection<ElectricEmailMailbox>(
    electricCollectionOptions({
      id: `sync-email-mailboxes-${accountId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "email_mailboxes",
          where: `account_id = '${accountId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.account_id)}:${String(item.email_id)}:${String(item.mailbox_id)}`,
    }) as any,
  );
}

// --- Email body collection ---

export interface ElectricEmailBody {
  account_id: string;
  email_id: string;
  text_body: string;
  html_body: string;
  fetched_at: string;
}

export function createEmailBodyCollection(accountId: string, emailId: string) {
  return createCollection<ElectricEmailBody>(
    electricCollectionOptions({
      id: `sync-email-body-${accountId}-${emailId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "email_bodies",
          where: `account_id = '${accountId}' AND email_id = '${emailId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.account_id)}:${String(item.email_id)}`,
    }) as any,
  );
}

// --- Thread collection ---

export interface ElectricThread {
  account_id: string;
  id: string;
  email_ids: string;
  synced_at: string;
}

export function createThreadCollection(accountId: string) {
  return createCollection<ElectricThread>(
    electricCollectionOptions({
      id: `sync-threads-${accountId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "threads",
          where: `account_id = '${accountId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.account_id)}:${String(item.id)}`,
    }) as any,
  );
}
