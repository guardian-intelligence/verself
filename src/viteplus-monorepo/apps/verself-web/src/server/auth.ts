import { createAuthConfig } from "@verself/auth-web/config";
import { deriveAuthIssuerURL, requireEnv, requireURLFromEnv } from "@verself/web-env";
import { readFileSync } from "node:fs";

function readEnvOrFile(name: string): string {
  const value = process.env[name]?.trim();
  if (value) {
    return value;
  }
  const path = process.env[`${name}_FILE`]?.trim();
  if (!path) {
    throw new Error(`${name} or ${name}_FILE is required`);
  }
  const fromFile = readFileSync(path, "utf8").trim();
  if (!fromFile) {
    throw new Error(`${name}_FILE is empty`);
  }
  return fromFile;
}

function readOptionalEnvOrFile(name: string): string | undefined {
  const value = process.env[name]?.trim();
  if (value) {
    return value;
  }
  const path = process.env[`${name}_FILE`]?.trim();
  if (!path) {
    return undefined;
  }
  const fromFile = readFileSync(path, "utf8").trim();
  if (!fromFile) {
    throw new Error(`${name}_FILE is empty`);
  }
  return fromFile;
}

function sessionDatabaseURL(): string {
  if (process.env.AUTH_DATABASE_URL?.trim()) {
    return requireURLFromEnv("AUTH_DATABASE_URL");
  }
  const url = new URL("postgresql://localhost");
  url.username = requireEnv("AUTH_DATABASE_USER");
  url.password = readEnvOrFile("AUTH_DATABASE_PASSWORD");
  url.hostname = requireEnv("AUTH_DATABASE_HOST");
  const port = process.env.AUTH_DATABASE_PORT?.trim();
  if (port) {
    url.port = port;
  }
  url.pathname = `/${requireEnv("AUTH_DATABASE_NAME")}`;
  return url.toString();
}

export function getAuthConfig() {
  const authProjectID = requireEnv("AUTH_PROJECT_ID");
  const identityAudience = process.env.IDENTITY_SERVICE_AUTH_AUDIENCE?.trim();
  return createAuthConfig({
    appName: "console",
    issuerURL: deriveAuthIssuerURL(),
    clientID: readEnvOrFile("AUTH_CLIENT_ID"),
    clientSecret:
      process.env.NODE_ENV === "production"
        ? readEnvOrFile("AUTH_CLIENT_SECRET")
        : readOptionalEnvOrFile("AUTH_CLIENT_SECRET"),
    sessionDatabaseURL: sessionDatabaseURL(),
    sessionPassword: readEnvOrFile("AUTH_SESSION_SECRET"),
    scopes: [
      "openid",
      "profile",
      "email",
      "offline_access",
      "urn:zitadel:iam:user:resourceowner",
      `urn:zitadel:iam:org:project:id:${authProjectID}:aud`,
      ...(identityAudience ? [`urn:zitadel:iam:org:project:id:${identityAudience}:aud`] : []),
      "urn:zitadel:iam:org:projects:roles",
    ],
    callbackPath: "/callback",
    defaultRedirectPath: "/",
    postLogoutRedirectPath: "/",
  });
}
