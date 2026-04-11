import * as v from "valibot";
import { authCollectionId, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import {
  createElectricShapeCollection,
  electricEqualsWhere,
  electricStringifiedBooleanSchema,
  electricStringifiedIntegerSchema,
  requireElectricOpaqueID,
} from "@forge-metal/web-env";

// --- Mailbox collection ---

const electricMailboxSchema = v.object({
  account_id: v.string(),
  id: v.string(),
  name: v.string(),
  parent_id: v.string(),
  role: v.string(),
  sort_order: electricStringifiedIntegerSchema,
  total_emails: electricStringifiedIntegerSchema,
  unread_emails: electricStringifiedIntegerSchema,
  total_threads: electricStringifiedIntegerSchema,
  unread_threads: electricStringifiedIntegerSchema,
  synced_at: v.string(),
});

export type ElectricMailbox = v.InferOutput<typeof electricMailboxSchema>;

export function createMailboxCollection(auth: AuthenticatedAuth, accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: authCollectionId(auth, `sync-mailboxes-${validatedAccountID}`),
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
  has_attachment: electricStringifiedBooleanSchema,
  size: electricStringifiedIntegerSchema,
  received_at: v.string(),
  sent_at: v.string(),
  is_seen: electricStringifiedBooleanSchema,
  is_flagged: electricStringifiedBooleanSchema,
  is_answered: electricStringifiedBooleanSchema,
  is_draft: electricStringifiedBooleanSchema,
  synced_at: v.string(),
});

export type ElectricEmail = v.InferOutput<typeof electricEmailSchema>;

export function createEmailCollection(auth: AuthenticatedAuth, accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: authCollectionId(auth, `sync-emails-${validatedAccountID}`),
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

export function createEmailMailboxCollection(auth: AuthenticatedAuth, accountId: string) {
  const validatedAccountID = requireElectricOpaqueID(accountId, "account_id");
  return createElectricShapeCollection({
    id: authCollectionId(auth, `sync-email-mailboxes-${validatedAccountID}`),
    schema: electricEmailMailboxSchema,
    table: "email_mailboxes",
    where: electricEqualsWhere("account_id", validatedAccountID),
    getKey: (item) => `${item.account_id}:${item.email_id}:${item.mailbox_id}`,
  });
}
