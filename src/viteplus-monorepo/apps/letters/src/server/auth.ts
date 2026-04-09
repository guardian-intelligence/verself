import { beginLogin, createAuthConfig, finishLogin, getAuthSession, getAuthUser, logout } from "@forge-metal/auth-web";

export const authConfig = createAuthConfig({
  appName: "letters",
  issuerURL: process.env.AUTH_ISSUER_URL ?? "",
  clientID: process.env.AUTH_CLIENT_ID ?? "",
  sessionDatabaseURL: process.env.AUTH_DATABASE_URL ?? "",
  sessionPassword: process.env.AUTH_SESSION_SECRET ?? "",
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
