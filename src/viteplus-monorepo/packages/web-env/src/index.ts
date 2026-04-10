import {
  electricCollectionOptions,
  type ElectricCollectionUtils,
} from "@tanstack/electric-db-collection";
import {
  createCollection,
  type Collection,
  type InferSchemaOutput,
  type NonSingleResult,
} from "@tanstack/react-db";
import * as v from "valibot";

export type EnvSource = Record<string, string | undefined>;
type LocationLike = { origin?: string };
type StandardIssue = {
  readonly message: string;
  readonly path?: ReadonlyArray<PropertyKey | { readonly key: PropertyKey }> | undefined;
};
type StandardResult<Output> =
  | {
      readonly issues?: undefined;
      readonly value: Output;
    }
  | {
      readonly issues: ReadonlyArray<StandardIssue>;
    };
type StandardSchemaLike<Input = unknown, Output = Input> = {
  readonly "~standard": {
    readonly validate: (
      value: unknown,
      options?: { readonly libraryOptions?: Record<string, unknown> | undefined },
    ) => StandardResult<Output> | Promise<StandardResult<Output>>;
    readonly types?: {
      readonly input: Input;
      readonly output: Output;
    } | undefined;
    readonly vendor: string;
    readonly version: 1;
  };
};


const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const digitsPattern = /^\d+$/;
const electricIntegerPattern = /^-?\d+$/;
const electricBooleanPattern = /^(?:true|false)$/;
const electricOpaqueIDPattern = /^[A-Za-z0-9._:-]+$/;

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
  return deriveHTTPSOrigin(appSubdomain, requireOperatorDomain("FORGE_METAL_DOMAIN", env));
}

export function deriveSeededEmail(env: EnvSource = process.env, localPart = "acme-user"): string {
  const explicitEmail = readEnv(env, "TEST_EMAIL");
  if (explicitEmail) {
    return explicitEmail;
  }
  return `${localPart}@${requireOperatorDomain("FORGE_METAL_DOMAIN", env)}`;
}

// Electric requires an absolute shape URL. Keep the real sync path same-origin
// in the browser, but return a harmless absolute fallback during SSR so the URL
// parser never sees a bare relative path.
export function electricShapeURL(): string {
  const location = (globalThis as { location?: LocationLike }).location;
  if (location?.origin) {
    return new URL("/v1/shape", location.origin).toString();
  }
  return "http://127.0.0.1/v1/shape";
}

export function requireUUID(value: string, label: string): string {
  const trimmed = value.trim();
  if (!uuidPattern.test(trimmed)) {
    throw new Error(`${label} must be a UUID`);
  }
  return trimmed;
}

export function requireDecimalID(value: string, label: string): string {
  const trimmed = value.trim();
  if (!digitsPattern.test(trimmed)) {
    throw new Error(`${label} must be a decimal identifier`);
  }
  return trimmed;
}

export function requireElectricOpaqueID(value: string, label: string): string {
  const trimmed = value.trim();
  if (!electricOpaqueIDPattern.test(trimmed)) {
    throw new Error(`${label} contains unsupported characters`);
  }
  return trimmed;
}

// Electric serializes PostgreSQL ints and booleans as strings in payloads.
export const electricStringifiedIntegerSchema = v.union([
  v.pipe(v.number(), v.integer()),
  v.pipe(v.string(), v.regex(electricIntegerPattern), v.transform(Number)),
]);

// Electric serializes PostgreSQL booleans as "true"/"false" strings in payloads.
export const electricStringifiedBooleanSchema = v.union([
  v.boolean(),
  v.pipe(v.string(), v.regex(electricBooleanPattern), v.transform((value) => value === "true")),
]);

export function electricEqualsWhere(column: string, validatedValue: string): string {
  return `${column} = '${validatedValue}'`;
}

export function electricAndWhere(clauses: Array<{ column: string; value: string }>): string {
  return clauses.map(({ column, value }) => electricEqualsWhere(column, value)).join(" AND ");
}

export function createElectricShapeCollection<
  TSchema extends StandardSchemaLike<Record<string, unknown>, Record<string, unknown>>,
>({
  id,
  table,
  schema,
  where,
  getKey,
}: {
  id: string;
  table: string;
  schema: TSchema;
  where?: string;
  getKey: (item: InferSchemaOutput<TSchema>) => string | number;
}): Collection<
  InferSchemaOutput<TSchema>,
  string | number,
  ElectricCollectionUtils<InferSchemaOutput<TSchema>>
> &
  NonSingleResult {
  const options = electricCollectionOptions({
    id,
    schema,
    shapeOptions: {
      url: electricShapeURL(),
      params: where ? { table, where } : { table },
    },
    getKey,
  });

  return createCollection<
    TSchema,
    string | number,
    ElectricCollectionUtils<InferSchemaOutput<TSchema>>
  >(options);
}
