const idempotencyKeyMaxLength = 128;

export type IdempotencyHeaders = { "Idempotency-Key": string };

type ServiceApiErrorConstructor<TError extends Error> = new (
  status: number,
  path: string,
  body: string,
) => TError;

export interface BearerClientOptions {
  accessToken: string;
  baseUrl: string;
  fetch?: typeof fetch;
}

export class ServiceApiError extends Error {
  constructor(
    serviceName: string,
    public readonly status: number,
    public readonly path: string,
    public readonly body: string,
  ) {
    super(`${serviceName} ${status}: ${body}`);
    this.name = "ServiceApiError";
  }
}

export function createBearerJSONHeaders(accessToken: string): Headers {
  const headers = new Headers();
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${accessToken}`);
  return headers;
}

function stringifyErrorBody(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object") {
    const detail = "detail" in error ? error.detail : undefined;
    if (typeof detail === "string" && detail) return detail;
    const title = "title" in error ? error.title : undefined;
    if (typeof title === "string" && title) return title;
    return JSON.stringify(error);
  }
  return String(error);
}

export function throwGeneratedServiceError<TError extends Error>(
  errorConstructor: ServiceApiErrorConstructor<TError>,
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  if (!response) {
    throw error instanceof Error ? error : new Error(stringifyErrorBody(error));
  }
  throw new errorConstructor(response.status, path, stringifyErrorBody(error));
}

export function createIdempotencyKey(namespace: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `${namespace}:${suffix}`.slice(0, idempotencyKeyMaxLength);
}

export function idempotencyHeaders(namespace: string): IdempotencyHeaders {
  return { "Idempotency-Key": createIdempotencyKey(namespace) };
}
