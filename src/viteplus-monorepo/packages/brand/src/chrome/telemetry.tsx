import { createContext, useContext, type ReactNode } from "react";

// Brand-package telemetry context. The brand package must stay router- and
// app-agnostic, but AppChrome wants to emit otel spans when it mounts and
// when the wordmark is clicked. Rather than import an app-specific emitSpan
// directly (which would couple @forge-metal/brand to @forge-metal/company),
// the chrome reads an injectable EmitSpan from context. The app wires its
// own emitSpan (which in turn wraps @opentelemetry/api) at __root.tsx and
// the brand layer remains a leaf dependency.
//
// Default: no-op. A brand component rendered outside a provider does not
// throw and does not produce spans.

export type EmitSpan = (name: string, attrs: Record<string, string>) => void;

const noop: EmitSpan = () => {};

const BrandTelemetryContext = createContext<EmitSpan>(noop);

export function BrandTelemetryProvider({
  emitSpan,
  children,
}: {
  readonly emitSpan: EmitSpan;
  readonly children: ReactNode;
}) {
  return (
    <BrandTelemetryContext.Provider value={emitSpan}>{children}</BrandTelemetryContext.Provider>
  );
}

export function useBrandTelemetry(): EmitSpan {
  return useContext(BrandTelemetryContext);
}
