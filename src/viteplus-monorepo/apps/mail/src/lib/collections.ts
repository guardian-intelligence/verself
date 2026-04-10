import * as v from "valibot";
import {
  createElectricShapeCollection,
  electricAndWhere,
  electricEqualsWhere,
  requireElectricOpaqueID,
} from "@forge-metal/web-env";

// --- Mailbox collection ---

const electricMailboxSchema = v.object({
  account_id: v.string(),
  id: v.string(),
  name: v.string(),
  parent_id: v.string(),
  role: v.string(),
  sort_order: v.number(),
  total_emails: v.number(),
  unread_emails: v.number(),
  total_threads: v.number(),
  unread_threads: v.number(),
  synced_at: v.string(),
});

export type ElectricMailbox = v.InferOutput<typeof electricMailboxSchema>;

export function createMailboxCollection(accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: `sync-mailboxes-${accountId}`,
    schema: electricMailboxSchema,
    table: "mailboxes",
    where: electricEqualsWhere("account_id", validatedAccountID),
    getKey: (item) => `${item.account_id}:${item.id}`,
  });
}

// --- Email collection ---

const electricEmailSchema = v.object({
  account_id: v.string(),
  id: v.string(),
  blob_id: v.string(),
  thread_id: v.string(),
  subject: v.string(),
  from_name: v.string(),
  from_email: v.string(),
  to_list: v.string(),
  cc_list: v.string(),
  reply_to_list: v.string(),
  preview: v.string(),
  keywords: v.string(),
  has_attachment: v.boolean(),
  size: v.number(),
  received_at: v.string(),
  sent_at: v.string(),
  is_seen: v.boolean(),
  is_flagged: v.boolean(),
  is_answered: v.boolean(),
  is_draft: v.boolean(),
  synced_at: v.string(),
});

export type ElectricEmail = v.InferOutput<typeof electricEmailSchema>;

export function createEmailCollection(accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: `sync-emails-${accountId}`,
    schema: electricEmailSchema,
    table: "emails",
    where: electricEqualsWhere("account_id", validatedAccountID),
    getKey: (item) => `${item.account_id}:${item.id}`,
  });
}

// --- Email-mailbox junction collection ---

const electricEmailMailboxSchema = v.object({
  account_id: v.string(),
  email_id: v.string(),
  mailbox_id: v.string(),
});

export type ElectricEmailMailbox = v.InferOutput<typeof electricEmailMailboxSchema>;

export function createEmailMailboxCollection(accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: `sync-email-mailboxes-${accountId}`,
    schema: electricEmailMailboxSchema,
    table: "email_mailboxes",
    where: electricEqualsWhere("account_id", validatedAccountID),
    getKey: (item) => `${item.account_id}:${item.email_id}:${item.mailbox_id}`,
  });
}

// --- Email body collection ---

const electricEmailBodySchema = v.object({
  account_id: v.string(),
  email_id: v.string(),
  text_body: v.string(),
  html_body: v.string(),
  fetched_at: v.string(),
});

export type ElectricEmailBody = v.InferOutput<typeof electricEmailBodySchema>;

export function createEmailBodyCollection(accountId: string, emailId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  const validatedEmailID = requireElectricOpaqueID(emailId, "email_id");
  return createElectricShapeCollection({
    id: `sync-email-body-${accountId}-${emailId}`,
    schema: electricEmailBodySchema,
    table: "email_bodies",
    where: electricAndWhere([
      { column: "account_id", value: validatedAccountID },
      { column: "email_id", value: validatedEmailID },
    ]),
    getKey: (item) => `${item.account_id}:${item.email_id}`,
  });
}

// --- Thread collection ---

const electricThreadSchema = v.object({
  account_id: v.string(),
  id: v.string(),
  email_ids: v.string(),
  synced_at: v.string(),
});

export type ElectricThread = v.InferOutput<typeof electricThreadSchema>;

export function createThreadCollection(accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: `sync-threads-${accountId}`,
    schema: electricThreadSchema,
    table: "threads",
    where: electricEqualsWhere("account_id", validatedAccountID),
    getKey: (item) => `${item.account_id}:${item.id}`,
  });
}
