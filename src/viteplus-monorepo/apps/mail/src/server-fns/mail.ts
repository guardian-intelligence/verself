import { createServerFn } from "@tanstack/react-start";
import { requireURLFromEnv } from "@forge-metal/web-env";
import { webmailAuthMiddleware } from "./auth";

const MAILBOX_SERVICE_BASE_URL = requireURLFromEnv("MAILBOX_SERVICE_BASE_URL");

async function mailboxServiceRequest<T>(
  accessToken: string,
  path: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${accessToken}`);
  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(new URL(path, MAILBOX_SERVICE_BASE_URL), {
    ...init,
    headers,
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    if (response.status === 404) {
      throw new Error("Not found");
    }
    throw new Error(`Mailbox API ${response.status}: ${body}`);
  }

  return response.json() as Promise<T>;
}

export interface MailAccount {
  account_id: string;
  email_address: string;
  display_name: string;
}

export interface EmailBody {
  account_id: string;
  email_id: string;
  text_body: string;
  html_body: string;
  fetched_at: string;
}

interface MailMutationResponse {
  status: string;
  email_id: string;
}

export const getMailAccount = createServerFn({ method: "GET" })
  .middleware([webmailAuthMiddleware])
  .handler(async ({ context }) => {
    try {
      return await mailboxServiceRequest<MailAccount>(
        context.auth.accessToken,
        "/api/v1/mail/account",
      );
    } catch (error) {
      if (error instanceof Error && error.message === "Not found") {
        return null;
      }
      throw error;
    }
  });

export const getEmailBody = createServerFn({ method: "GET" })
  .middleware([webmailAuthMiddleware])
  .inputValidator((data: { emailId: string }) => data)
  .handler(async ({ context, data }) => {
    return mailboxServiceRequest<EmailBody>(
      context.auth.accessToken,
      `/api/v1/mail/emails/${data.emailId}/body`,
    );
  });

export const markEmailRead = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator((data: { emailId: string }) => data)
  .handler(async ({ context, data }) => {
    return mailboxServiceRequest<MailMutationResponse>(
      context.auth.accessToken,
      `/api/v1/mail/emails/${data.emailId}/read`,
      { method: "POST" },
    );
  });

export const flagEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator((data: { emailId: string }) => data)
  .handler(async ({ context, data }) => {
    return mailboxServiceRequest<MailMutationResponse>(
      context.auth.accessToken,
      `/api/v1/mail/emails/${data.emailId}/flag`,
      { method: "POST" },
    );
  });

export const unflagEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator((data: { emailId: string }) => data)
  .handler(async ({ context, data }) => {
    return mailboxServiceRequest<MailMutationResponse>(
      context.auth.accessToken,
      `/api/v1/mail/emails/${data.emailId}/unflag`,
      { method: "POST" },
    );
  });

export const trashEmail = createServerFn({ method: "POST" })
  .middleware([webmailAuthMiddleware])
  .inputValidator((data: { emailId: string }) => data)
  .handler(async ({ context, data }) => {
    return mailboxServiceRequest<MailMutationResponse>(
      context.auth.accessToken,
      `/api/v1/mail/emails/${data.emailId}/trash`,
      { method: "POST" },
    );
  });
