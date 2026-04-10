import { createServerFn } from "@tanstack/react-start";
import { requireURLFromEnv } from "@forge-metal/web-env";
import {
  emailIdInputSchema,
  flagEmail as flagEmailRequest,
  getEmailBody as getEmailBodyRequest,
  getMailAccount as getMailAccountRequest,
  MailboxApiError,
  markEmailRead as markEmailReadRequest,
  trashEmail as trashEmailRequest,
  unflagEmail as unflagEmailRequest,
} from "~/lib/mailbox-api";
import type { EmailBody, MailAccount, MailMutationResponse } from "~/lib/mailbox-api";
import { webmailAuthMiddleware } from "./auth";

const MAILBOX_SERVICE_BASE_URL = requireURLFromEnv("MAILBOX_SERVICE_BASE_URL");

export type { EmailBody, MailAccount };

export type MailAccountResult =
  | { status: "ok"; account: MailAccount }
  | { status: "no_binding" }
  | { status: "not_found" };

function mailboxClientOptions(context: { auth: { accessToken: string } }) {
  return {
    accessToken: context.auth.accessToken,
    baseUrl: MAILBOX_SERVICE_BASE_URL,
  };
}

export const getMailAccount = createServerFn({ method: "GET" })
  .middleware([webmailAuthMiddleware])
  .handler(async ({ context }): Promise<MailAccountResult> => {
    try {
      const account = await getMailAccountRequest(mailboxClientOptions(context));
      return { status: "ok", account };
    } catch (error) {
      if (error instanceof MailboxApiError) {
        if (error.status === 403) return { status: "no_binding" };
        if (error.status === 404) return { status: "not_found" };
      }
      throw error;
    }
  });

export const getEmailBody = createServerFn({ method: "GET" })
  .middleware([webmailAuthMiddleware])
  .inputValidator(emailIdInputSchema)
  .handler(async ({ context, data }) => {
    return getEmailBodyRequest({
      ...mailboxClientOptions(context),
      emailId: data.emailId,
    });
  });

export const markEmailRead = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator(emailIdInputSchema)
  .handler(async ({ context, data }): Promise<MailMutationResponse> => {
    return markEmailReadRequest({
      ...mailboxClientOptions(context),
      emailId: data.emailId,
    });
  });

export const flagEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator(emailIdInputSchema)
  .handler(async ({ context, data }): Promise<MailMutationResponse> => {
    return flagEmailRequest({
      ...mailboxClientOptions(context),
      emailId: data.emailId,
    });
  });

export const unflagEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator(emailIdInputSchema)
  .handler(async ({ context, data }): Promise<MailMutationResponse> => {
    return unflagEmailRequest({
      ...mailboxClientOptions(context),
      emailId: data.emailId,
    });
  });

export const trashEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator(emailIdInputSchema)
  .handler(async ({ context, data }): Promise<MailMutationResponse> => {
    return trashEmailRequest({
      ...mailboxClientOptions(context),
      emailId: data.emailId,
    });
  });
