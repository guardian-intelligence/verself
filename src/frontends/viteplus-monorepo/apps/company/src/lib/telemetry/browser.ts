import { trace, type Span, type SpanOptions } from "@opentelemetry/api";
import { ZoneContextManager } from "@opentelemetry/context-zone";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { BatchSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { WebTracerProvider } from "@opentelemetry/sdk-trace-web";
import {
  ATTR_SERVICE_NAME,
  ATTR_DEPLOYMENT_ENVIRONMENT_NAME,
} from "@opentelemetry/semantic-conventions/incubating";
import { DEPLOY_META, RESOURCE_ATTR_KEYS } from "./meta-keys";

const TRACER_NAME = "verself/company-web";
const TRACER_VERSION = "0.1.0";
// Same-origin proxy in apps/company/src/routes/api/otel/v1/traces.ts. The
// company CSP pins connect-src 'self', which forbids the browser from posting
// directly to the otel collector at 127.0.0.1:4318 — proxy is mandatory.
const TRACES_ENDPOINT = "/api/otel/v1/traces";

let provider: WebTracerProvider | undefined;

function readMetaContent(name: string): string {
  if (typeof document === "undefined") {
    return "";
  }
  const el = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`);
  return el?.content?.trim() ?? "";
}

function buildResourceAttributes() {
  const attrs: Record<string, string> = {
    [ATTR_SERVICE_NAME]: "company-web",
    [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: "production",
  };
  const runKey = readMetaContent(DEPLOY_META.runKey);
  const id = readMetaContent(DEPLOY_META.id);
  const commitSha = readMetaContent(DEPLOY_META.commitSha);
  const supervisor = readMetaContent(DEPLOY_META.supervisor);
  if (runKey) attrs[RESOURCE_ATTR_KEYS.runKey] = runKey;
  if (id) attrs[RESOURCE_ATTR_KEYS.id] = id;
  if (commitSha) attrs[RESOURCE_ATTR_KEYS.commitSha] = commitSha;
  if (supervisor) attrs[RESOURCE_ATTR_KEYS.supervisor] = supervisor;
  return attrs;
}

// Initialise the browser tracer. Idempotent so HMR re-imports don't stack
// providers. The BatchSpanProcessor flushes on visibilitychange:hidden via the
// listener below; the OTLP HTTP exporter uses fetch keepalive so the request
// survives a tab close.
export function initBrowserTelemetry(): void {
  if (typeof window === "undefined") return;
  if (provider) return;

  const exporter = new OTLPTraceExporter({ url: TRACES_ENDPOINT });
  const next = new WebTracerProvider({
    resource: resourceFromAttributes(buildResourceAttributes()),
    spanProcessors: [
      new BatchSpanProcessor(exporter, {
        maxQueueSize: 200,
        maxExportBatchSize: 50,
        scheduledDelayMillis: 2_000,
      }),
    ],
  });
  next.register({ contextManager: new ZoneContextManager() });
  provider = next;

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") {
      void next.forceFlush();
    }
  });
}

export function getTracer() {
  return trace.getTracer(TRACER_NAME, TRACER_VERSION);
}

export function withSpan<T>(name: string, attrs: Record<string, string>, fn: (span: Span) => T): T {
  const opts: SpanOptions = { attributes: attrs };
  return getTracer().startActiveSpan(name, opts, (span) => {
    try {
      const result = fn(span);
      span.end();
      return result;
    } catch (error) {
      span.recordException(error as Error);
      span.setStatus({ code: 2, message: (error as Error).message });
      span.end();
      throw error;
    }
  });
}

export function emitSpan(name: string, attrs: Record<string, string>): void {
  const span = getTracer().startSpan(name, { attributes: attrs });
  span.end();
}
