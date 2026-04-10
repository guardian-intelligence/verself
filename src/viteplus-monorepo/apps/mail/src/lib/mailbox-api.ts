import * as v from "valibot";
import { createClient, type Client } from "../__generated/mailbox-api/client/index.js";
import {
  mailAccount,
  mailBody,
  mailFlag as mailFlagMutation,
  mailMarkRead as mailMarkReadMutation,
  mailTrash as mailTrashMutation,
  mailUnflag as mailUnflagMutation,
} from "../__generated/mailbox-api/index.js";
import {
  vMailAccountResponse,
  vMailBodyResponse,
  vMailBodyPath,
  vMailMutationOutputBody,
} from "../__generated/mailbox-api/valibot.gen.js";

export interface MailboxClientOptions {
  accessToken: string;
  baseUrl: string;
  fetch?: typeof fetch;
}

export class MailboxApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly path: string,
    public readonly body: string,
  ) {
    super(`Mailbox API ${status}: ${body}`);
    this.name = "MailboxApiError";
  }
}

export function isMailboxApiError(error: unknown): error is MailboxApiError {
  return error instanceof MailboxApiError;
}

function stringifyErrorBody(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object") {
    const detail = "detail" in error ? error.detail : undefined;
    if (typeof detail === "string" && detail) return detail;
    const title = "title" in error ? error.title : undefined;
    if (typeof title === "string" && title) return title;
    return JSON.stringify(error);
  }
  return String(error);
}

function throwMailboxError(path: string, response: Response | undefined, error: unknown): never {
  if (!response) {
    throw error instanceof Error ? error : new Error(stringifyErrorBody(error));
  }
  throw new MailboxApiError(response.status, path, stringifyErrorBody(error));
}

function createMailboxClient(options: MailboxClientOptions): Client {
  const headers = new Headers();
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${options.accessToken}`);

  return createClient({
    baseUrl: options.baseUrl,
    headers,
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function parseMailAccount(input: unknown) {
  const { $schema: _schema, ...account } = v.parse(vMailAccountResponse, input);
  return account;
}

export type MailAccount = ReturnType<typeof parseMailAccount>;

function parseEmailBody(input: unknown) {
  const { $schema: _schema, ...body } = v.parse(vMailBodyResponse, input);
  return body;
}

export type EmailBody = ReturnType<typeof parseEmailBody>;

function parseMailMutationResponse(input: unknown) {
  const { $schema: _schema, ...response } = v.parse(vMailMutationOutputBody, input);
  return response;
}

export type MailMutationResponse = ReturnType<typeof parseMailMutationResponse>;

export const emailIdInputSchema = v.pipe(
  v.strictObject({
    emailId: v.string(),
  }),
  v.transform(({ emailId }) => ({
    emailId: v.parse(vMailBodyPath, { email_id: emailId }).email_id,
  })),
);

export type EmailIdInput = v.InferOutput<typeof emailIdInputSchema>;

export async function getMailAccount(options: MailboxClientOptions): Promise<MailAccount> {
  const client = createMailboxClient(options);
  const path = "/api/v1/mail/account";
  const result = await mailAccount({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseMailAccount(result.data);
}

export async function getEmailBody(
  options: MailboxClientOptions & { emailId: string },
): Promise<EmailBody> {
  const client = createMailboxClient(options);
  const { emailId } = v.parse(emailIdInputSchema, { emailId: options.emailId });
  const path = `/api/v1/mail/emails/${emailId}/body`;
  const result = await mailBody({
    client,
    path: { email_id: emailId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseEmailBody(result.data);
}

export async function markEmailRead(
  options: MailboxClientOptions & { emailId: string },
): Promise<MailMutationResponse> {
  const client = createMailboxClient(options);
  const { emailId } = v.parse(emailIdInputSchema, { emailId: options.emailId });
  const path = `/api/v1/mail/emails/${emailId}/read`;
  const result = await mailMarkReadMutation({
    client,
    path: { email_id: emailId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseMailMutationResponse(result.data);
}

export async function flagEmail(
  options: MailboxClientOptions & { emailId: string },
): Promise<MailMutationResponse> {
  const client = createMailboxClient(options);
  const { emailId } = v.parse(emailIdInputSchema, { emailId: options.emailId });
  const path = `/api/v1/mail/emails/${emailId}/flag`;
  const result = await mailFlagMutation({
    client,
    path: { email_id: emailId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseMailMutationResponse(result.data);
}

export async function unflagEmail(
  options: MailboxClientOptions & { emailId: string },
): Promise<MailMutationResponse> {
  const client = createMailboxClient(options);
  const { emailId } = v.parse(emailIdInputSchema, { emailId: options.emailId });
  const path = `/api/v1/mail/emails/${emailId}/unflag`;
  const result = await mailUnflagMutation({
    client,
    path: { email_id: emailId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseMailMutationResponse(result.data);
}

export async function trashEmail(
  options: MailboxClientOptions & { emailId: string },
): Promise<MailMutationResponse> {
  const client = createMailboxClient(options);
  const { emailId } = v.parse(emailIdInputSchema, { emailId: options.emailId });
  const path = `/api/v1/mail/emails/${emailId}/trash`;
  const result = await mailTrashMutation({
    client,
    path: { email_id: emailId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwMailboxError(path, result.response, result.error);
  }

  return parseMailMutationResponse(result.data);
}
