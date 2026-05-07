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

export * from "./time";

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
    readonly types?:
      | {
          readonly input: Input;
          readonly output: Output;
        }
      | undefined;
    readonly vendor: string;
    readonly version: 1;
  };
};

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const digitsPattern = /^\d+$/;
const electricIntegerPattern = /^-?\d+$/;
const electricBooleanPattern = /^(?:true|false)$/;
const electricOpaqueIDPattern = /^[A-Za-z0-9._:-]+$/;
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

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

export function parseProductDomain(value: string, label: string): string {
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

export function requireProductDomain(
  envName = "VERSELF_DOMAIN",
  env: EnvSource = process.env,
): string {
  return parseProductDomain(requireEnv(envName, env), envName);
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
  const normalizedDomain = parseProductDomain(domain, "domain");
  return new URL(`https://${normalizedSubdomain}.${normalizedDomain}`).toString();
}

export function deriveProductBaseURL(env: EnvSource = process.env): string {
  const explicitBaseURL = readEnv(env, "PRODUCT_BASE_URL");
  if (explicitBaseURL) {
    return parseAbsoluteURL(explicitBaseURL, "PRODUCT_BASE_URL");
  }
  return new URL(`https://${requireProductDomain("VERSELF_DOMAIN", env)}`).toString();
}

export function deriveAuthIssuerURL(env: EnvSource = process.env): string {
  const authSubdomain = readEnv(env, "AUTH_SUBDOMAIN") ?? "auth";
  return deriveHTTPSOrigin(authSubdomain, requireProductDomain("VERSELF_DOMAIN", env));
}

export function deriveAppBaseURL(appSubdomain: string, env: EnvSource = process.env): string {
  const explicitBaseURL = readEnv(env, "BASE_URL");
  if (explicitBaseURL) {
    return parseAbsoluteURL(explicitBaseURL, "BASE_URL");
  }
  return deriveHTTPSOrigin(appSubdomain, requireProductDomain("VERSELF_DOMAIN", env));
}

export function deriveSeededEmail(env: EnvSource = process.env, localPart = "acme-user"): string {
  const explicitEmail = readEnv(env, "TEST_EMAIL");
  if (explicitEmail) {
    return explicitEmail;
  }
  return `${localPart}@${requireProductDomain("VERSELF_DOMAIN", env)}`;
}

// Electric requires an absolute shape URL. The browser only receives named
// same-origin resources; table, columns, and authorization predicates are
// constructed by the server-side proxy.
export function electricShapeURL(path: string): string {
  const location = (globalThis as { location?: LocationLike }).location;
  if (location?.origin) {
    return new URL(path, location.origin).toString();
  }
  return `http://127.0.0.1${path}`;
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

function requireSafeElectricInteger(value: number): number {
  if (!Number.isSafeInteger(value)) {
    throw new Error("Electric integer exceeds Number.MAX_SAFE_INTEGER");
  }
  return value;
}

function parseElectricBigInt(value: bigint): number {
  if (value > maxSafeInteger || value < -maxSafeInteger) {
    throw new Error("Electric integer exceeds Number.MAX_SAFE_INTEGER");
  }
  return Number(value);
}

function parseElectricInteger(value: string): number {
  const parsed = BigInt(value);
  if (parsed > maxSafeInteger || parsed < -maxSafeInteger) {
    throw new Error("Electric integer exceeds Number.MAX_SAFE_INTEGER");
  }
  return Number(parsed);
}

// Electric serializes PostgreSQL ints and booleans as strings in payloads.
export const electricStringifiedIntegerSchema = v.union([
  v.pipe(v.bigint(), v.transform(parseElectricBigInt)),
  v.pipe(v.number(), v.integer(), v.transform(requireSafeElectricInteger)),
  v.pipe(v.string(), v.regex(electricIntegerPattern), v.transform(parseElectricInteger)),
]);

// Electric serializes PostgreSQL booleans as "true"/"false" strings in payloads.
export const electricStringifiedBooleanSchema = v.union([
  v.boolean(),
  v.pipe(
    v.string(),
    v.regex(electricBooleanPattern),
    v.transform((value) => value === "true"),
  ),
]);

export function createElectricShapeCollection<
  TSchema extends StandardSchemaLike<Record<string, unknown>, Record<string, unknown>>,
>({
  id,
  schema,
  getKey,
  shapePath,
}: {
  id: string;
  schema: TSchema;
  getKey: (item: InferSchemaOutput<TSchema>) => string | number;
  shapePath: string;
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
      url: electricShapeURL(shapePath),
      params: {},
      subsetMethod: "POST",
    },
    getKey,
  });

  return createCollection<
    TSchema,
    string | number,
    ElectricCollectionUtils<InferSchemaOutput<TSchema>>
  >(options);
}
