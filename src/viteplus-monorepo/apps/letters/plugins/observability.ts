import { trace } from "@opentelemetry/api";
import { definePlugin } from "nitro";
import type { H3Event } from "nitro/h3";

const requestStartKey = "forge_metal_request_started_at_ns";

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

export default definePlugin((nitroApp) => {
  nitroApp.hooks.hook("request", (event) => {
    const h3Event = event as H3Event;
    const context = h3Event.context as Record<string, unknown>;
    context[requestStartKey] = process.hrtime.bigint();
  });

  nitroApp.hooks.hook("response", (_response, event) => {
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
    const statusCode = h3Event.res.status ?? 200;
    const level = statusCode >= 500 ? "error" : statusCode >= 400 ? "warn" : "info";

    emitLog(level, "http request completed", {
      trace_id: spanContext?.traceId ?? "",
      span_id: spanContext?.spanId ?? "",
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

    emitLog("error", "nitro request failed", {
      trace_id: spanContext?.traceId ?? "",
      span_id: spanContext?.spanId ?? "",
      http_method: h3Event?.req.method ?? "",
      http_target: url ? `${url.pathname}${url.search}` : "",
      url_path: url?.pathname ?? "",
      error_name: error.name,
      error_message: error.message,
      error_stack: error.stack ?? "",
    });
  });
});
