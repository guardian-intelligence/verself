import * as v from "valibot";
import { createClient, type Client } from "./__generated/secrets-api/client/index.js";
import {
  type DeleteSecretData,
  type ListSecretsData,
  type PutSecretData,
  type ResolveSecretsData,
  deleteSecret,
  listSecrets,
  putSecret,
  resolveSecrets,
} from "./__generated/secrets-api/index.js";
import {
  vDeleteSecretPath,
  vDeleteSecretQuery,
  vDeleteSecretResponse,
  vListSecretsQuery,
  vListSecretsResponse,
  vPutSecretBody,
  vPutSecretPath,
  vPutSecretResponse,
  vResolveSecretsBody,
  vResolveSecretsResponse,
  vSecretDto,
  vSecretValueDto,
} from "./__generated/secrets-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type SecretsClientOptions = BearerClientOptions;

export type SecretsMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class SecretsApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Secrets API", status, path, body);
    this.name = "SecretsApiError";
  }
}

export function isSecretsApiError(error: unknown): error is SecretsApiError {
  return error instanceof SecretsApiError;
}

function createSecretsClient(options: SecretsClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwSecretsError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(SecretsApiError, path, response, error);
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

function parseSecret(input: unknown) {
  return v.parse(vSecretDto, input);
}

export type Secret = ReturnType<typeof parseSecret>;

function parseSecretValue(input: unknown) {
  return v.parse(vSecretValueDto, input);
}

export type SecretValue = ReturnType<typeof parseSecretValue>;

function parseSecretList(input: unknown) {
  const parsed = v.parse(vListSecretsResponse, input);
  return {
    secrets: parsed.secrets.map((secret) => parseSecret(secret)),
  };
}

export type SecretList = ReturnType<typeof parseSecretList>;

function parseResolvedSecrets(input: unknown) {
  const parsed = v.parse(vResolveSecretsResponse, input);
  return {
    values: parsed.values.map((secret) => parseSecretValue(secret)),
  };
}

export type ResolvedSecrets = ReturnType<typeof parseResolvedSecrets>;

export const putSecretRequestSchema = vPutSecretBody;
export const resolveSecretsRequestSchema = vResolveSecretsBody;

export type PutSecretRequest = v.InferOutput<typeof putSecretRequestSchema>;
export type ResolveSecretsRequest = v.InferOutput<typeof resolveSecretsRequestSchema>;

export type DeleteSecretScope = NonNullable<DeleteSecretData["query"]>;
type PutSecretRequestBody = PutSecretData["body"];
type ResolveSecretsRequestBody = NonNullable<ResolveSecretsData["body"]>;

export type ListSecretsInput = {
  limit?: number | undefined;
};

export class Secrets {
  readonly #options: SecretsClientOptions;

  constructor(options: SecretsClientOptions) {
    this.#options = options;
  }

  async list(input: ListSecretsInput = {}): Promise<SecretList> {
    const client = createSecretsClient(this.#options);
    const query = removeUndefined(v.parse(vListSecretsQuery, input)) as NonNullable<
      ListSecretsData["query"]
    >;
    const path = "/api/v1/secrets";
    const result = await listSecrets({
      client,
      query,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSecretsError(path, result.response, result.error);
    }
    return parseSecretList(result.data);
  }

  async put(
    name: string,
    body: PutSecretRequest,
    options: SecretsMutationOptions = {},
  ): Promise<Secret> {
    const client = createSecretsClient(this.#options);
    const pathParams = v.parse(vPutSecretPath, { name });
    const parsedBody = removeUndefined(v.parse(vPutSecretBody, body)) as PutSecretRequestBody;
    const path = `/api/v1/secrets/${encodeURIComponent(name)}`;
    const result = await putSecret({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("secret", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSecretsError(path, result.response, result.error);
    }
    return parseSecret(v.parse(vPutSecretResponse, result.data));
  }

  async resolve(body: ResolveSecretsRequest = {}): Promise<ResolvedSecrets> {
    const client = createSecretsClient(this.#options);
    const parsed = removeUndefined(v.parse(vResolveSecretsBody, body));
    if (typeof parsed.limit === "bigint") {
      parsed.limit = Number(parsed.limit);
    }
    const parsedBody = parsed as ResolveSecretsRequestBody;
    const path = "/api/v1/secrets:resolve";
    const result = await resolveSecrets({
      client,
      body: parsedBody,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSecretsError(path, result.response, result.error);
    }
    return parseResolvedSecrets(result.data);
  }

  async delete(
    name: string,
    scope: DeleteSecretScope,
    options: SecretsMutationOptions = {},
  ): Promise<Secret> {
    const client = createSecretsClient(this.#options);
    const pathParams = v.parse(vDeleteSecretPath, { name });
    const query = removeUndefined(v.parse(vDeleteSecretQuery, scope)) as DeleteSecretScope;
    const path = `/api/v1/secrets/${encodeURIComponent(name)}`;
    const result = await deleteSecret({
      client,
      path: pathParams,
      query,
      headers: idempotencyHeaders("secret", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSecretsError(path, result.response, result.error);
    }
    return parseSecret(v.parse(vDeleteSecretResponse, result.data));
  }
}
