import { beginLogin, createAuthConfig, finishLogin, getAuthSession, getAuthUser, logout } from "@forge-metal/auth-web";
import { deriveAuthIssuerURL, requireEnv, requireURLFromEnv } from "@forge-metal/web-env";

export const authConfig = createAuthConfig({
  appName: "webmail",
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

export function beginWebmailLogin(redirectTo?: string | null) {
  return beginLogin(authConfig, redirectTo);
}

export function finishWebmailLogin() {
  return finishLogin(authConfig);
}

export function getWebmailAuthSession() {
  return getAuthSession(authConfig);
}

export function getWebmailAuthUser() {
  return getAuthUser(authConfig);
}

export function logoutWebmail() {
  return logout(authConfig);
}
