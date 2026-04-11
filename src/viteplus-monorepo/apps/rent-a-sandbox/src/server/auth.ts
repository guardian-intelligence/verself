import { createAuthConfig } from "@forge-metal/auth-web/config";
import { deriveAuthIssuerURL, requireEnv, requireURLFromEnv } from "@forge-metal/web-env";

export function getAuthConfig() {
  const authProjectID = requireEnv("AUTH_PROJECT_ID");
  const identityProjectID = process.env.IDENTITY_SERVICE_AUTH_PROJECT_ID?.trim();
  return createAuthConfig({
    appName: "rent-a-sandbox",
    issuerURL: deriveAuthIssuerURL(),
    clientID: requireEnv("AUTH_CLIENT_ID"),
    clientSecret: process.env.AUTH_CLIENT_SECRET?.trim() || undefined,
    sessionDatabaseURL: requireURLFromEnv("AUTH_DATABASE_URL"),
    sessionPassword: requireEnv("AUTH_SESSION_SECRET"),
    scopes: [
      "openid",
      "profile",
      "email",
      "offline_access",
      "urn:zitadel:iam:user:resourceowner",
      `urn:zitadel:iam:org:project:id:${authProjectID}:aud`,
      ...(identityProjectID ? [`urn:zitadel:iam:org:project:id:${identityProjectID}:aud`] : []),
      "urn:zitadel:iam:org:projects:roles",
    ],
    callbackPath: "/callback",
    defaultRedirectPath: "/",
    postLogoutRedirectPath: "/",
  });
}
