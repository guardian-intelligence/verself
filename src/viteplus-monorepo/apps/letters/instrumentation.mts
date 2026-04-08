import { getNodeAutoInstrumentations } from "@opentelemetry/auto-instrumentations-node";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { NodeSDK } from "@opentelemetry/sdk-node";

const serviceName = process.env.OTEL_SERVICE_NAME || "letters";
const otlpEndpoint = (process.env.OTEL_EXPORTER_OTLP_ENDPOINT || "http://127.0.0.1:4318").replace(
  /\/+$/,
  "",
);
const tracesEndpoint =
  process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT || `${otlpEndpoint}/v1/traces`;

const sdk = new NodeSDK({
  resource: resourceFromAttributes({
    "service.name": serviceName,
    "deployment.environment.name": process.env.NODE_ENV || "production",
  }),
  traceExporter: new OTLPTraceExporter({ url: tracesEndpoint }),
  instrumentations: [
    getNodeAutoInstrumentations({
      "@opentelemetry/instrumentation-fs": { enabled: false },
    }),
  ],
});

await sdk.start();

let shuttingDown = false;

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
