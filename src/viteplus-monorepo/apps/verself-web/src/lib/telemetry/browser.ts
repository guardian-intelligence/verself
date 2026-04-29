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

import { registerFetchInstrumentation } from "./fetch-instrumentation";
import { DEPLOY_META, RESOURCE_ATTR_KEYS } from "./meta-keys";

const TRACER_NAME = "verself/verself-web";
const TRACER_VERSION = "0.1.0";
// Same-origin proxy in apps/verself-web/src/routes/api/otel/v1/traces.tsx. The
// app's CSP pins connect-src 'self', which forbids the browser from posting
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
    [ATTR_SERVICE_NAME]: "verself-web",
    [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: "production",
  };
  const runKey = readMetaContent(DEPLOY_META.runKey);
  const id = readMetaContent(DEPLOY_META.id);
  const commitSha = readMetaContent(DEPLOY_META.commitSha);
  const profile = readMetaContent(DEPLOY_META.profile);
  if (runKey) attrs[RESOURCE_ATTR_KEYS.runKey] = runKey;
  if (id) attrs[RESOURCE_ATTR_KEYS.id] = id;
  if (commitSha) attrs[RESOURCE_ATTR_KEYS.commitSha] = commitSha;
  if (profile) attrs[RESOURCE_ATTR_KEYS.profile] = profile;
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
        // Web defaults from the OpenTelemetry JS docs. Tuned for short page
        // visits where the BSP must drain before the user navigates away.
        maxQueueSize: 200,
        maxExportBatchSize: 50,
        scheduledDelayMillis: 2_000,
      }),
    ],
  });
  next.register({ contextManager: new ZoneContextManager() });
  provider = next;

  // FetchInstrumentation is the load-bearing wire from a user interaction to
  // a server span: it wraps `window.fetch`, so every TanStack Start server
  // function carries a `traceparent` header derived from the active span
  // context. The server-side HttpInstrumentation in nitro-plugins reads that
  // header and makes the resulting Nitro request span a child of the
  // browser-emitted span.
  //
  // `registerFetchInstrumentation` is `createClientOnlyFn(...)`-wrapped:
  // TanStack Start's compiler plugin replaces it with `() => undefined` for
  // the SSR build (and tree-shakes the OTel CJS chain out of the SSR
  // bundle), so this call is a no-op during server rendering. On the client
  // it's a synchronous static-imported invocation, so there is no async
  // window where an outbound fetch could fire without traceparent injection.
  registerFetchInstrumentation();

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") {
      // Best-effort flush; BSP returns a Promise but we deliberately don't
      // await — the page is on its way out and the exporter uses keepalive.
      void next.forceFlush();
    }
  });
}

export function getTracer() {
  return trace.getTracer(TRACER_NAME, TRACER_VERSION);
}

// Helper to bracket a synchronous block as a span with structured attributes
// without forcing every caller to import the OTel API. Errors set status=ERROR.
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
