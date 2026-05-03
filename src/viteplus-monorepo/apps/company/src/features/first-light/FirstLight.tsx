"use client";

import { lazy, Suspense, useCallback, useRef, useState, type RefObject } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import type { DegradedReason } from "./types";
import {
  useFirstLightActive,
  useFirstLightGeometry,
  useFirstLightRuntime,
  useTrailLuminance,
} from "./use-first-light";

const FirstLightCanvas = lazy(() =>
  import("./scene/FirstLightCanvas").then((module) => ({ default: module.FirstLightCanvas })),
);

export interface FirstLightProps {
  readonly trailTargetRef: RefObject<HTMLElement | null>;
  readonly wingsAnchorRef: RefObject<HTMLElement | null>;
  readonly motion?: boolean;
}

export function FirstLight({ trailTargetRef, wingsAnchorRef, motion }: FirstLightProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [canvasDegradedReason, setCanvasDegradedReason] = useState<DegradedReason>();
  const runtime = useFirstLightRuntime(motion);
  const geometry = useFirstLightGeometry(hostRef, trailTargetRef, wingsAnchorRef);
  const active = useFirstLightActive(hostRef);
  const fallbackReason = runtime.kind === "fallback" ? runtime.reason : canvasDegradedReason;
  const live =
    runtime.kind === "ready" && geometry !== undefined && canvasDegradedReason === undefined;
  useTrailLuminance(trailTargetRef, live);
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
      {runtime.kind === "ready" && geometry && !canvasDegradedReason ? (
        <Suspense fallback={null}>
          <FirstLightCanvas active={active} geometry={geometry} onDegraded={handleCanvasDegraded} />
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
          "radial-gradient(42% 34% at 28% 22%, rgba(255, 250, 240, 0.2), transparent 62%), linear-gradient(132deg, transparent 18%, rgba(125, 190, 255, 0.08) 37%, rgba(255, 250, 240, 0.16) 48%, rgba(255, 176, 110, 0.08) 56%, transparent 72%)",
        mixBlendMode: "screen",
      }}
    />
  );
}
