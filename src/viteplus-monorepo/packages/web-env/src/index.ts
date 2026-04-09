export type EnvSource = Record<string, string | undefined>;

function readEnv(env: EnvSource, name: string): string | undefined {
  return env[name]?.trim();
}

export function requireEnv(name: string, env: EnvSource = process.env): string {
  const value = readEnv(env, name);
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

export function parseAbsoluteURL(value: string, label: string): string {
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`${label} must be an absolute URL`);
  }

  if (!parsed.protocol || !parsed.hostname) {
    throw new Error(`${label} must be an absolute URL`);
  }

  return parsed.toString();
}

export function requireURLFromEnv(name: string, env: EnvSource = process.env): string {
  return parseAbsoluteURL(requireEnv(name, env), name);
}

export function parseOperatorDomain(value: string, label: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    throw new Error(`${label} is required`);
  }
  if (trimmed.includes("://")) {
    throw new Error(`${label} must be a bare domain, not a URL`);
  }
  if (/[/?#@]/.test(trimmed)) {
    throw new Error(`${label} must be a bare domain`);
  }
  if (trimmed.includes(":")) {
    throw new Error(`${label} must not include a port`);
  }

  let parsed: URL;
  try {
    parsed = new URL(`https://${trimmed}`);
  } catch {
    throw new Error(`${label} must be a valid domain`);
  }

  if (parsed.username || parsed.password || parsed.port) {
    throw new Error(`${label} must be a bare domain`);
  }

  return parsed.hostname;
}

export function requireOperatorDomain(
  envName = "FORGE_METAL_DOMAIN",
  env: EnvSource = process.env,
): string {
  return parseOperatorDomain(requireEnv(envName, env), envName);
}

function parseSubdomain(value: string, label: string): string {
  const trimmed = value.trim().toLowerCase();
  if (!trimmed) {
    throw new Error(`${label} is required`);
  }
  if (!/^[a-z0-9-]+(?:\.[a-z0-9-]+)*$/.test(trimmed)) {
    throw new Error(`${label} must be a valid subdomain`);
  }
  return trimmed;
}

export function deriveHTTPSOrigin(subdomain: string, domain: string): string {
  const normalizedSubdomain = parseSubdomain(subdomain, "subdomain");
  const normalizedDomain = parseOperatorDomain(domain, "domain");
  return new URL(`https://${normalizedSubdomain}.${normalizedDomain}`).toString();
}

export function deriveAuthIssuerURL(env: EnvSource = process.env): string {
  const authSubdomain = readEnv(env, "AUTH_SUBDOMAIN") ?? "auth";
  return deriveHTTPSOrigin(authSubdomain, requireOperatorDomain("FORGE_METAL_DOMAIN", env));
}

export function deriveAppBaseURL(appSubdomain: string, env: EnvSource = process.env): string {
  const explicitBaseURL = readEnv(env, "BASE_URL");
  if (explicitBaseURL) {
    return parseAbsoluteURL(explicitBaseURL, "BASE_URL");
  }
  return deriveHTTPSOrigin(
    appSubdomain,
    requireOperatorDomain("FORGE_METAL_DOMAIN", env),
  );
}

export function deriveDemoEmail(env: EnvSource = process.env, localPart = "demo"): string {
  const explicitEmail = readEnv(env, "TEST_EMAIL");
  if (explicitEmail) {
    return explicitEmail;
  }
  return `${localPart}@${requireOperatorDomain("FORGE_METAL_DOMAIN", env)}`;
}
