"use client";

import { lazy, Suspense, useCallback, useRef, useState } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import type { DegradedReason } from "./types";
import { useFirstLightActive, useFirstLightFrame, useFirstLightRuntime } from "./use-first-light";

const FirstLightCanvas = lazy(() =>
  import("./scene/FirstLightCanvas").then((module) => ({ default: module.FirstLightCanvas })),
);

export interface FirstLightProps {
  readonly motion?: boolean;
}

export function FirstLight({ motion }: FirstLightProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [canvasDegradedReason, setCanvasDegradedReason] = useState<DegradedReason>();
  const runtime = useFirstLightRuntime(motion);
  const frame = useFirstLightFrame(hostRef);
  const active = useFirstLightActive(hostRef);
  const fallbackReason = runtime.kind === "fallback" ? runtime.reason : canvasDegradedReason;
  const live =
    runtime.kind === "ready" && frame !== undefined && canvasDegradedReason === undefined;
  const handleCanvasDegraded = useCallback((reason: DegradedReason, error?: unknown) => {
    setCanvasDegradedReason(reason);
    emitSpan("company.first_light.degraded", {
      reason,
      ...errorAttrs(error),
    });
  }, []);

  return (
    <div
      ref={hostRef}
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-0 overflow-hidden"
    >
      {fallbackReason ? <FirstLightStill reason={fallbackReason} /> : null}
      {live ? (
        <Suspense fallback={null}>
          <FirstLightCanvas active={active} frame={frame} onDegraded={handleCanvasDegraded} />
        </Suspense>
      ) : null}
    </div>
  );
}

function errorAttrs(error: unknown): Record<string, string> {
  if (!(error instanceof Error)) {
    return {};
  }
  return {
    "error.name": error.name,
    "error.message": error.message.slice(0, 256),
  };
}

function FirstLightStill({ reason }: { readonly reason: DegradedReason }) {
  const opacity = reason === "reduced_motion" ? 0.58 : 0.38;
  return (
    <div
      data-firstlight-fallback={reason}
      className="absolute inset-0"
      style={{
        opacity,
        background:
          "radial-gradient(32% 42% at 74% 16%, rgba(255, 238, 204, 0.34), transparent 58%), linear-gradient(132deg, transparent 14%, rgba(104, 184, 216, 0.1) 32%, rgba(255, 250, 240, 0.18) 48%, rgba(255, 176, 110, 0.12) 56%, transparent 74%)",
        mixBlendMode: "screen",
      }}
    />
  );
}
