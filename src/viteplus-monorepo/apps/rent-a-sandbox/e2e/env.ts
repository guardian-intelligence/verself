import { deriveAppBaseURL, deriveAuthIssuerURL, deriveDemoEmail } from "@forge-metal/web-env";

// Test environment configuration. All values can be overridden via env vars.
function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(
      `${name} is required. Seed the demo user and provide the stored password explicitly.`,
    );
  }
  return value;
}

export const env = {
  baseURL: deriveAppBaseURL("rentasandbox"),
  testEmail: deriveDemoEmail(),
  testPassword: requiredEnv("TEST_PASSWORD"),
  testUsername: process.env.TEST_USERNAME || "demo",
  testFirstName: process.env.TEST_FIRST_NAME || "Demo",
  testLastName: process.env.TEST_LAST_NAME || "User",

  // Zitadel admin PAT — optional. If set, the test will auto-provision the
  // test user via the Zitadel Management API. If not set, the user must
  // already exist (e.g., via `make seed-demo`).
  zitadelAdminPAT: process.env.ZITADEL_ADMIN_PAT || "",
  zitadelBaseURL: process.env.ZITADEL_BASE_URL?.trim() || deriveAuthIssuerURL(),
  zitadelProjectName: process.env.ZITADEL_PROJECT_NAME || "sandbox-rental",

  verificationRunID: process.env.VERIFICATION_RUN_ID || "",
  verificationRunJSONPath: process.env.VERIFICATION_RUN_JSON_PATH || "",
  verificationRepoURL:
    process.env.VERIFICATION_REPO_URL || "http://127.0.0.1:3000/forge-admin/sandbox-verification-next-bun.git",
  verificationRepoRef: process.env.VERIFICATION_REPO_REF || "refs/heads/main",
  verificationLogMarker:
    process.env.VERIFICATION_LOG_MARKER || "FORGE_METAL_VERIFICATION_NEXT_BUN_COMPLETE",

  // Stripe test card — always succeeds, no 3DS challenge.
  stripeCard: "4242424242424242",
  stripeExpiry: "12/30",
  stripeCVC: "123",
} as const;
