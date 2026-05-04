// FetchInstrumentation registration, gated as client-only via TanStack
// Start's `createClientOnlyFn`. The compiler plugin replaces the function
// body with `() => undefined` on the SSR side and tree-shakes the static
// imports in this file out of the SSR bundle, so OTel's CJS chain
// (require-in-the-middle, module-details-from-path) never reaches Nitro's
// external-deps tracer at build time. On the client, the call is a
// synchronous static-imported invocation, so there is no async window
// where an outbound fetch could fire without `traceparent` injection.
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { FetchInstrumentation } from "@opentelemetry/instrumentation-fetch";
import { createClientOnlyFn } from "@tanstack/react-start";

export const registerFetchInstrumentation = createClientOnlyFn(() => {
  registerInstrumentations({
    instrumentations: [
      new FetchInstrumentation({
        // CSP `connect-src 'self'` already pins fetches to the same origin,
        // so this regex only matters as defence-in-depth: if a future call
        // site ever targets a cross-origin URL on our own apex, traceparent
        // attaches without us reasoning about CORS-safelist semantics.
        propagateTraceHeaderCorsUrls: [new RegExp(`^${window.location.origin}`)],
      }),
    ],
  });
});
