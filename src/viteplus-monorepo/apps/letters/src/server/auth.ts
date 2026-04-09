import { beginLogin, createAuthConfig, finishLogin, getAuthSession, getAuthUser, logout } from "@forge-metal/auth-web";
import { deriveAuthIssuerURL, requireEnv, requireURLFromEnv } from "@forge-metal/web-env";

export const authConfig = createAuthConfig({
  appName: "letters",
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

export function beginLettersLogin(redirectTo?: string | null) {
  return beginLogin(authConfig, redirectTo);
}

export function finishLettersLogin() {
  return finishLogin(authConfig);
}

export function getLettersAuthSession() {
  return getAuthSession(authConfig);
}

export function getLettersAuthUser() {
  return getAuthUser(authConfig);
}

export function logoutLetters() {
  return logout(authConfig);
}
