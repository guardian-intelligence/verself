import { createFileRoute } from "@tanstack/react-router";

// Same-origin OTLP HTTP forwarder. The browser cannot reach the otel collector
// directly: CSP `connect-src 'self'` blocks cross-origin POSTs and the
// collector binds to 127.0.0.1:4318 (per src/platform/ansible/group_vars/all/services.yml),
// so this server route accepts OTLP-JSON POSTs and forwards them upstream.
//
// Body cap: 256 KiB. The web SDK's BatchSpanProcessor batches up to 50 spans
// before exporting; even with attribute-heavy spans we expect single-digit KB
// per request. Anything larger is suspicious and rejected loudly.

const COLLECTOR_TRACES_URL =
  process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ??
  `${(process.env.OTEL_EXPORTER_OTLP_ENDPOINT ?? "http://127.0.0.1:4318").replace(/\/+$/, "")}/v1/traces`;

const MAX_BODY_BYTES = 256 * 1024;

export const Route = createFileRoute("/api/otel/v1/traces")({
  server: {
    handlers: {
      POST: async ({ request }) => {
        const contentType = request.headers.get("content-type") ?? "";
        if (!contentType.startsWith("application/json")) {
          return new Response("expected application/json", {
            status: 415,
            headers: { "content-type": "text/plain" },
          });
        }

        const body = await request.arrayBuffer();
        if (body.byteLength === 0) {
          return new Response("empty body", {
            status: 400,
            headers: { "content-type": "text/plain" },
          });
        }
        if (body.byteLength > MAX_BODY_BYTES) {
          return new Response("payload too large", {
            status: 413,
            headers: { "content-type": "text/plain" },
          });
        }

        try {
          const upstream = await fetch(COLLECTOR_TRACES_URL, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body,
          });
          // The OTel collector returns 200 with empty body on success and
          // 4xx/5xx with a JSON ExportTracePartialSuccess message on partial.
          // Pass status through so the browser exporter can react.
          return new Response(upstream.body, {
            status: upstream.status,
            headers: {
              "content-type": upstream.headers.get("content-type") ?? "application/json",
            },
          });
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          return new Response(`otelcol unreachable: ${message}`, {
            status: 502,
            headers: { "content-type": "text/plain" },
          });
        }
      },
    },
  },
});
