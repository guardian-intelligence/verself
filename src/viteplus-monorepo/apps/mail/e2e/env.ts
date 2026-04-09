import { deriveAppBaseURL, deriveAuthIssuerURL, deriveSeededEmail } from "@forge-metal/web-env";

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(
      `${name} is required. Seed the CEO user and provide the stored password explicitly.`,
    );
  }
  return value;
}

export const env = {
  baseURL: deriveAppBaseURL("mail"),
  testEmail: deriveSeededEmail(process.env, "ceo"),
  testPassword: requiredEnv("TEST_PASSWORD"),
  testUsername: process.env.TEST_USERNAME || "ceo",
  testFirstName: process.env.TEST_FIRST_NAME || "CEO",
  testLastName: process.env.TEST_LAST_NAME || "Operator",
  inboxLocalPart: process.env.MAIL_INBOX_LOCAL_PART || "ceo",

  zitadelAdminPAT: process.env.ZITADEL_ADMIN_PAT || "",
  zitadelBaseURL: process.env.ZITADEL_BASE_URL?.trim() || deriveAuthIssuerURL(),
  zitadelProjectName: process.env.ZITADEL_PROJECT_NAME || "mailbox-service",
} as const;
