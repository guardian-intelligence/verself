import { deriveAppBaseURL, deriveAuthIssuerURL, deriveSeededEmail } from "@verself/web-env";

// Test environment configuration. All values can be overridden via env vars.
function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(
      `${name} is required. Seed the test user and provide the stored password explicitly.`,
    );
  }
  return value;
}

export const env = {
  baseURL: deriveAppBaseURL("console"),
  testEmail: deriveSeededEmail(process.env, "ceo"),
  testPassword: requiredEnv("TEST_PASSWORD"),
  testUsername: process.env.TEST_USERNAME || "ceo",
  testFirstName: process.env.TEST_FIRST_NAME || "CEO",
  testLastName: process.env.TEST_LAST_NAME || "Operator",

  // Zitadel admin PAT — optional. If set, the test will auto-provision the
  // test user via the Zitadel Management API. If not set, the user must
  // already exist (e.g., via `make seed-system`).
  zitadelAdminPAT: process.env.ZITADEL_ADMIN_PAT || "",
  zitadelBaseURL: process.env.ZITADEL_BASE_URL?.trim() || deriveAuthIssuerURL(),
  zitadelProjectName: process.env.ZITADEL_PROJECT_NAME || "sandbox-rental",

  verificationRunID: process.env.VERIFICATION_RUN_ID || "",
  verificationRunJSONPath: process.env.VERIFICATION_RUN_JSON_PATH || "",
  verificationRepoURL:
    process.env.VERIFICATION_REPO_URL ||
    `https://git.${process.env.VERSELF_DOMAIN || "verself.sh"}/forgejo-automation/sandbox-verification-metadata.git`,
  verificationRepoRef: process.env.VERIFICATION_REPO_REF || "refs/heads/main",
  verificationLogMarker: process.env.VERIFICATION_LOG_MARKER || "VERSELF_DIRECT_EXECUTION_COMPLETE",
  proofMode: process.env.VERSELF_SANDBOX_PROOF === "1",

  // Stripe test card — always succeeds, no 3DS challenge.
  stripeCard: "4242424242424242",
  stripeExpiry: "12/30",
  stripeCVC: "123",
} as const;
