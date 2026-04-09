import { createAuthConfig } from "@forge-metal/auth-web";
import { deriveAuthIssuerURL, requireEnv, requireURLFromEnv } from "@forge-metal/web-env";

export const authConfig = createAuthConfig({
  appName: "rent-a-sandbox",
  issuerURL: deriveAuthIssuerURL(),
  clientID: requireEnv("AUTH_CLIENT_ID"),
  sessionDatabaseURL: requireURLFromEnv("AUTH_DATABASE_URL"),
  sessionPassword: requireEnv("AUTH_SESSION_SECRET"),
  scopes: [
    "openid",
    "profile",
    "email",
    "offline_access",
    "urn:zitadel:iam:user:resourceowner",
  ],
  callbackPath: "/callback",
  defaultRedirectPath: "/",
  postLogoutRedirectPath: "/",
});
