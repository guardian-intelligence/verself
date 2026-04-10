export interface AuthConfig {
  appName: string;
  issuerURL: string;
  clientID: string;
  clientSecret?: string;
  sessionCookieName?: string;
  sessionDatabaseURL: string;
  sessionPassword: string;
  sessionMaxAgeSeconds?: number;
  refreshLeewaySeconds?: number;
  scopes: string[];
  callbackPath: string;
  defaultRedirectPath: string;
  postLogoutRedirectPath: string;
}

export type AuthConfigSource = AuthConfig | (() => AuthConfig);
export type AsyncAuthConfigSource = AuthConfig | (() => AuthConfig | Promise<AuthConfig>);

function requiredNonEmpty(value: string | undefined, label: string): string {
  const trimmed = value?.trim();
  if (!trimmed) {
    throw new Error(`${label} is required`);
  }
  return trimmed;
}

export function createAuthConfig(config: AuthConfig): AuthConfig {
  return {
    ...config,
    issuerURL: requiredNonEmpty(config.issuerURL, `${config.appName} issuerURL`),
    clientID: requiredNonEmpty(config.clientID, `${config.appName} clientID`),
    sessionDatabaseURL: requiredNonEmpty(
      config.sessionDatabaseURL,
      `${config.appName} sessionDatabaseURL`,
    ),
    sessionPassword: requiredNonEmpty(config.sessionPassword, `${config.appName} sessionPassword`),
    sessionCookieName: config.sessionCookieName ?? `${config.appName}-session`,
    sessionMaxAgeSeconds: config.sessionMaxAgeSeconds ?? 60 * 60 * 24 * 30,
    refreshLeewaySeconds: config.refreshLeewaySeconds ?? 60,
  };
}

export function resolveAuthConfig(config: AuthConfigSource): AuthConfig {
  return typeof config === "function" ? config() : config;
}

export async function resolveAuthConfigAsync(config: AsyncAuthConfigSource): Promise<AuthConfig> {
  return typeof config === "function" ? config() : config;
}
