import { getRequest } from "@tanstack/react-start/server";

const forwardedRequestMetadataHeaderNames = [
  "Origin",
  "Referer",
  "User-Agent",
  "X-Request-ID",
  "X-Verself-Client-IP",
  "X-Verself-Client-IP-Source",
  "X-Verself-Edge-Peer-IP",
] as const;

export function currentRequestHeaders(): Headers {
  return getRequest().headers;
}

export function currentCookieHeader(sourceHeaders: Headers | undefined): string | undefined {
  return sourceHeaders?.get("cookie") ?? undefined;
}

export function forwardRequestMetadata(headers: Headers, sourceHeaders: Headers | undefined): void {
  if (!sourceHeaders) return;
  for (const name of forwardedRequestMetadataHeaderNames) {
    const value = sourceHeaders.get(name);
    if (value) {
      headers.set(name, value);
    }
  }
}

export function requestMetadataFetch(
  sourceHeaders: Headers | undefined = currentRequestHeaders(),
): typeof fetch {
  return ((input: RequestInfo | URL, init?: RequestInit) => {
    if (!sourceHeaders) {
      return fetch(input, init);
    }
    const inputHeaders = input instanceof Request ? input.headers : undefined;
    const headers = new Headers(init?.headers ?? inputHeaders);
    forwardRequestMetadata(headers, sourceHeaders);
    return fetch(input, { ...init, headers });
  }) as typeof fetch;
}
