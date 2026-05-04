import { trace } from "@opentelemetry/api";
import { definePlugin } from "nitro";
import type { H3Event } from "nitro/h3";

import { correlationMiddleware } from "./correlation-middleware.ts";
import { correlationContextKey, correlationHeaderName } from "./correlation.ts";

const requestStartKey = "verself_request_started_at_ns";

function correlationIDFromContext(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "bigint" || typeof value === "boolean") {
    return String(value);
  }
  return "";
}

function emitLog(
  level: "debug" | "info" | "warn" | "error",
  msg: string,
  fields: Record<string, string | number>,
) {
  const payload = {
    time: new Date().toISOString(),
    level,
    msg,
    ...fields,
  };
  const line = JSON.stringify(payload);

  if (level === "error") {
    console.error(line);
    return;
  }

  if (level === "warn") {
    console.warn(line);
    return;
  }

  console.log(line);
}

function responseStatusCode(response: unknown, event: H3Event): number {
  if (response !== null && typeof response === "object" && "status" in response) {
    const status = (response as { status?: unknown }).status;
    if (typeof status === "number" && status > 0) {
      return status;
    }
  }

  return event.res.status ?? 200;
}

// Extracts the TanStack Start server-function ID out of a request URL so the
// trace shows which fn handled the request. Server fns POST to
// `/_serverFn/<functionId>`; anything else returns "" and we don't decorate.
function serverFnIDFromPath(pathname: string): string {
  const prefix = "/_serverFn/";
  if (!pathname.startsWith(prefix)) {
    return "";
  }
  const tail = pathname.slice(prefix.length);
  const slash = tail.indexOf("/");
  return slash === -1 ? tail : tail.slice(0, slash);
}

export default definePlugin((nitroApp) => {
  nitroApp.hooks.hook("request", (event) => {
    const h3Event = event as H3Event;
    // Keep correlation minting at the Nitro edge so same-origin app traffic
    // gets a stable cookie without contaminating cross-origin OIDC requests.
    correlationMiddleware(h3Event);
    const context = h3Event.context as Record<string, unknown>;
    context[requestStartKey] = process.hrtime.bigint();

    // HttpInstrumentation already created the SERVER span and extracted any
    // browser-emitted `traceparent`. Decorate it with attributes that map to
    // domain concepts — correlation ID, route path, server fn name — so the
    // ClickHouse trace tree groups by the intent the user expressed in the
    // browser. We never start a span here; the http instrumentation owns
    // lifecycle.
    const span = trace.getActiveSpan();
    if (!span) return;
    const correlationID =
      h3Event.req.headers.get(correlationHeaderName.toLowerCase()) ??
      correlationIDFromContext(context[correlationContextKey]);
    span.setAttribute("verself.correlation_id", correlationID);
    span.setAttribute("verself.route", h3Event.url.pathname);
    const serverFnID = serverFnIDFromPath(h3Event.url.pathname);
    if (serverFnID !== "") {
      span.setAttribute("verself.server_fn", serverFnID);
    }
  });

  nitroApp.hooks.hook("response", (response, event) => {
    const h3Event = event as H3Event;
    const context = h3Event.context as Record<string, unknown>;
    const requestStartedAt = context[requestStartKey];
    const durationMs =
      typeof requestStartedAt === "bigint"
        ? Number((process.hrtime.bigint() - requestStartedAt) / 1000000n)
        : 0;
    const span = trace.getActiveSpan();
    const spanContext = span?.spanContext();
    const url = h3Event.url;
    // Nitro can return a Response while event.res.status stays at the default 200.
    const statusCode = responseStatusCode(response, h3Event);
    const level = statusCode >= 500 ? "error" : statusCode >= 400 ? "warn" : "info";

    const correlationID =
      h3Event.req.headers.get(correlationHeaderName.toLowerCase()) ??
      correlationIDFromContext(context[correlationContextKey]);

    emitLog(level, "http request completed", {
      trace_id: spanContext?.traceId ?? "",
      span_id: spanContext?.spanId ?? "",
      verself_correlation_id: correlationID,
      http_method: h3Event.req.method,
      http_target: `${url.pathname}${url.search}`,
      url_path: url.pathname,
      http_status_code: statusCode,
      duration_ms: durationMs,
      user_agent: h3Event.req.headers.get("user-agent") ?? "",
      forwarded_for: h3Event.req.headers.get("x-forwarded-for") ?? "",
    });
  });

  nitroApp.hooks.hook("error", (error, context) => {
    const span = trace.getActiveSpan();
    const spanContext = span?.spanContext();
    const h3Event = context.event as H3Event | undefined;
    const url = h3Event?.url;
    const correlationID =
      h3Event?.req.headers.get(correlationHeaderName.toLowerCase()) ??
      correlationIDFromContext(
        (h3Event?.context as Record<string, unknown> | undefined)?.[correlationContextKey],
      );

    emitLog("error", "nitro request failed", {
      trace_id: spanContext?.traceId ?? "",
      span_id: spanContext?.spanId ?? "",
      verself_correlation_id: correlationID,
      http_method: h3Event?.req.method ?? "",
      http_target: url ? `${url.pathname}${url.search}` : "",
      url_path: url?.pathname ?? "",
      error_name: error.name,
      error_message: error.message,
      error_stack: error.stack ?? "",
    });
  });
});
