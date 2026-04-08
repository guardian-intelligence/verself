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
  baseURL: process.env.BASE_URL || "https://rentasandbox.anveio.com",
  testEmail: process.env.TEST_EMAIL || "demo@anveio.com",
  testPassword: requiredEnv("TEST_PASSWORD"),
  testUsername: process.env.TEST_USERNAME || "demo",
  testFirstName: process.env.TEST_FIRST_NAME || "Demo",
  testLastName: process.env.TEST_LAST_NAME || "User",

  // Zitadel admin PAT — optional. If set, the test will auto-provision the
  // test user via the Zitadel Management API. If not set, the user must
  // already exist (e.g., via `make seed-demo`).
  zitadelAdminPAT: process.env.ZITADEL_ADMIN_PAT || "",
  zitadelBaseURL: process.env.ZITADEL_BASE_URL || "https://auth.anveio.com",
  zitadelProjectName: process.env.ZITADEL_PROJECT_NAME || "sandbox-rental",

  // Stripe test card — always succeeds, no 3DS challenge.
  stripeCard: "4242424242424242",
  stripeExpiry: "12/30",
  stripeCVC: "123",
} as const;
