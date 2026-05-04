import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { HttpInstrumentation } from "@opentelemetry/instrumentation-http";
import { UndiciInstrumentation } from "@opentelemetry/instrumentation-undici";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { NodeSDK } from "@opentelemetry/sdk-node";

// Curated instrumentation set — deliberately not @opentelemetry/auto-
// instrumentations-node. The goal of OTel here is one specific wire: trace
// continuity from a browser interaction to the API behaviour it triggered.
// That wire only needs two patches:
//
//   HttpInstrumentation     — wraps the Node http server so incoming requests
//                             extract `traceparent` and the resulting span is
//                             a child of the browser's interaction span. Same
//                             instrumentation also covers outbound http.request.
//   UndiciInstrumentation   — Node 18+ `fetch()` runs on undici, not http.request,
//                             so it bypasses HttpInstrumentation. This patch
//                             injects `traceparent` on outbound fetch — the call
//                             path TanStack Start server functions use to reach
//                             our backend Go services.
//
// Everything else (request lifecycle attributes, correlation IDs, server-fn
// spans, page-view spans) is emitted explicitly from app code via the OTel
// API. No auto-patching of fs, dns, child_process, ioredis, kafka, …
function logSdkError(msg: string, error: unknown): void {
  const payload = {
    time: new Date().toISOString(),
    level: "error",
    msg,
    error_name: error instanceof Error ? error.name : typeof error,
    error_message: error instanceof Error ? error.message : String(error),
    error_stack: error instanceof Error && error.stack ? error.stack : "",
  };
  console.error(JSON.stringify(payload));
}

export async function initOtel(serviceName: string): Promise<void> {
  const otlpEndpoint = (process.env.OTEL_EXPORTER_OTLP_ENDPOINT || "http://127.0.0.1:4318").replace(
    /\/+$/,
    "",
  );
  const tracesEndpoint =
    process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT || `${otlpEndpoint}/v1/traces`;

  const sdk = new NodeSDK({
    resource: resourceFromAttributes({
      "service.name": process.env.OTEL_SERVICE_NAME || serviceName,
      "deployment.environment.name": process.env.NODE_ENV || "production",
    }),
    traceExporter: new OTLPTraceExporter({ url: tracesEndpoint }),
    instrumentations: [new HttpInstrumentation(), new UndiciInstrumentation()],
  });

  sdk.start();

  let shuttingDown = false;

  async function shutdown(signal: NodeJS.Signals): Promise<void> {
    if (shuttingDown) {
      return;
    }
    shuttingDown = true;

    try {
      await sdk.shutdown();
    } catch (error) {
      logSdkError("otel sdk shutdown failed", error);
    } finally {
      process.exit(signal === "SIGINT" ? 130 : 0);
    }
  }

  process.on("SIGINT", () => {
    void shutdown("SIGINT");
  });

  process.on("SIGTERM", () => {
    void shutdown("SIGTERM");
  });
}
