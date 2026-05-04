// Browser-side helper for bracketing user gestures with a span. Use this at
// the call sites that lead to API calls (form submits, mutations, route
// transitions kicked off by clicks); the span becomes the trace root and
// FetchInstrumentation's child fetch spans inherit traceparent from the
// active span context, so the server-side Nitro request span ends up as a
// grandchild of the interaction span.
//
// Skipped @opentelemetry/instrumentation-user-interaction deliberately: it
// patches every addEventListener globally and emits a span per click, which
// is noisy and hard to attribute. Wrapping the handlers we care about
// directly keeps the set of tracked gestures grep-able
// (`rg "withInteractionSpan\("` enumerates them).

import { type Attributes } from "@opentelemetry/api";

import { getTracer } from "./browser";

interface InteractionSpanOptions {
  attributes?: Attributes;
}

export function withInteractionSpan<TArgs extends unknown[], TRet>(
  name: string,
  fn: (...args: TArgs) => TRet | Promise<TRet>,
  options: InteractionSpanOptions = {},
): (...args: TArgs) => Promise<TRet> {
  return async (...args: TArgs) => {
    const tracer = getTracer();
    return tracer.startActiveSpan(
      `interaction.${name}`,
      { attributes: options.attributes ?? {} },
      async (span) => {
        try {
          const result = await fn(...args);
          return result;
        } catch (error) {
          span.recordException(error as Error);
          span.setStatus({ code: 2, message: (error as Error).message });
          throw error;
        } finally {
          // Hold the span across the microtask the fetch promise schedules,
          // so FetchInstrumentation child spans link to it before the
          // BatchSpanProcessor flushes. One paint frame is more than enough;
          // the BSP exports asynchronously after that.
          await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
          span.end();
        }
      },
    );
  };
}
