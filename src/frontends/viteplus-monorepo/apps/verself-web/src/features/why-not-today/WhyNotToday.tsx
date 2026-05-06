import { lazy, Suspense, useCallback, useRef, useState } from "react";
import type { DegradedReason } from "./types";
import { useSceneActive, useSceneFrame, useSceneRuntime } from "./use-why-not-today";

const WhyNotTodayCanvas = lazy(() =>
  import("./scene/WhyNotTodayCanvas").then((m) => ({ default: m.WhyNotTodayCanvas })),
);

export interface WhyNotTodayProps {
  readonly motion?: boolean;
}

export function WhyNotToday({ motion }: WhyNotTodayProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [canvasDegradedReason, setCanvasDegradedReason] = useState<DegradedReason>();
  const runtime = useSceneRuntime(motion);
  const frame = useSceneFrame(hostRef);
  const active = useSceneActive(hostRef);
  const fallbackReason = runtime.kind === "fallback" ? runtime.reason : canvasDegradedReason;
  const live =
    runtime.kind === "ready" && frame !== undefined && canvasDegradedReason === undefined;
  const handleCanvasDegraded = useCallback((reason: DegradedReason) => {
    setCanvasDegradedReason(reason);
  }, []);

  return (
    <div
      ref={hostRef}
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-0 overflow-hidden"
    >
      {fallbackReason ? <WhyNotTodayStill /> : null}
      {live ? (
        <Suspense fallback={null}>
          <WhyNotTodayCanvas active={active} frame={frame} onDegraded={handleCanvasDegraded} />
        </Suspense>
      ) : null}
    </div>
  );
}

function WhyNotTodayStill() {
  return (
    <div
      className="absolute inset-0"
      style={{
        background:
          "radial-gradient(60% 70% at 50% 86%, rgba(64, 168, 240, 0.42), transparent 70%), radial-gradient(80% 60% at 50% 30%, rgba(120, 86, 220, 0.22), transparent 70%)",
        mixBlendMode: "screen",
      }}
    />
  );
}
