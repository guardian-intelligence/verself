"use client";

import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";

import { cn } from "@verself/ui/lib/utils";

import { emitSpan } from "~/lib/telemetry/browser";
import type { DotMatrixDegradedReason, DotMatrixFrame, DotMatrixRendererBackend } from "./types";

const DotMatrixCanvas = lazy(() =>
  import("./DotMatrixCanvas").then((module) => ({ default: module.DotMatrixCanvas })),
);

type RuntimeState =
  | { readonly kind: "pending" }
  | { readonly backend: DotMatrixRendererBackend; readonly kind: "ready" }
  | {
      readonly backend: DotMatrixRendererBackend;
      readonly kind: "disabled";
      readonly reason: DotMatrixDegradedReason;
    };

interface DotMatrixFieldProps {
  readonly className?: string;
  readonly motion?: boolean;
}

export function DotMatrixField({ className, motion = true }: DotMatrixFieldProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [canvasReady, setCanvasReady] = useState(false);
  const [canvasDegradedReason, setCanvasDegradedReason] = useState<DotMatrixDegradedReason>();
  const runtime = useDotMatrixRuntime(motion);
  const frame = useDotMatrixFrame(hostRef);
  const active = useDotMatrixActive(hostRef);
  const live =
    runtime.kind === "ready" && frame !== undefined && canvasDegradedReason === undefined;

  const handleCanvasReady = useCallback(() => {
    setCanvasReady(true);
  }, []);

  const handleCanvasDegraded = useCallback((reason: DotMatrixDegradedReason, error?: unknown) => {
    setCanvasDegradedReason(reason);
    emitSpan("verself.landing_dot_matrix.degraded", {
      reason,
      ...errorAttrs(error),
    });
  }, []);

  return (
    <div
      ref={hostRef}
      aria-hidden="true"
      className={cn("pointer-events-none absolute inset-0 overflow-hidden", className)}
    >
      {live ? (
        <div
          className={cn(
            "absolute inset-0 transition-opacity duration-700 ease-out",
            canvasReady ? "opacity-100" : "opacity-0",
          )}
          data-dot-matrix-live={canvasReady ? "" : undefined}
        >
          <Suspense fallback={null}>
            <DotMatrixCanvas
              active={active}
              frame={frame}
              onDegraded={handleCanvasDegraded}
              onReady={handleCanvasReady}
            />
          </Suspense>
        </div>
      ) : null}
    </div>
  );
}

function useDotMatrixRuntime(motion: boolean): RuntimeState {
  const [state, setState] = useState<RuntimeState>({ kind: "pending" });

  useEffect(() => {
    if (typeof window === "undefined") return;

    let cancelled = false;
    let idleHandle: number | undefined;
    let frameHandle: number | undefined;

    const start = () => {
      if (cancelled) return;

      const reducedMotion =
        !motion || window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      const backend = reducedMotion ? "none" : detectRendererBackend();
      emitSpan("verself.landing_dot_matrix.capability", {
        renderer_backend: backend,
        prefers_reduced_motion: String(reducedMotion),
        device_pixel_ratio: String(window.devicePixelRatio || 1),
        "viewport.w": String(window.innerWidth),
        "viewport.h": String(window.innerHeight),
      });

      if (reducedMotion) {
        setState({ backend, kind: "disabled", reason: "reduced_motion" });
        return;
      }
      if (backend === "none") {
        setState({ backend, kind: "disabled", reason: "no_renderer" });
        return;
      }
      if (backend === "webgl2_no_float_render_target") {
        setState({ backend, kind: "disabled", reason: "float_render_target_unsupported" });
        return;
      }
      setState({ backend, kind: "ready" });
    };

    const scheduleStart = () => {
      const requestIdleCallback = window.requestIdleCallback;
      if (typeof requestIdleCallback === "function") {
        idleHandle = requestIdleCallback(start, { timeout: 900 });
        return;
      }
      frameHandle = window.requestAnimationFrame(start);
    };

    if (document.readyState === "complete") {
      scheduleStart();
    } else {
      window.addEventListener("load", scheduleStart, { once: true });
    }

    return () => {
      cancelled = true;
      window.removeEventListener("load", scheduleStart);
      if (idleHandle !== undefined) window.cancelIdleCallback(idleHandle);
      if (frameHandle !== undefined) window.cancelAnimationFrame(frameHandle);
    };
  }, [motion]);

  return state;
}

function useDotMatrixFrame(hostRef: RefObject<HTMLElement | null>): DotMatrixFrame | undefined {
  const [frame, setFrame] = useState<DotMatrixFrame>();

  useLayoutEffect(() => {
    if (typeof window === "undefined") return;

    const update = () => {
      const host = hostRef.current;
      if (!host) return;

      const hostBox = host.getBoundingClientRect();
      if (hostBox.width <= 0 || hostBox.height <= 0) return;

      setFrame({
        viewport: {
          dpr: window.devicePixelRatio || 1,
          h: hostBox.height,
          w: hostBox.width,
        },
      });
    };

    const frameHandle = window.requestAnimationFrame(update);
    const resizeObserver = new ResizeObserver(update);
    const host = hostRef.current;
    if (host) resizeObserver.observe(host);

    window.addEventListener("resize", update);
    return () => {
      window.cancelAnimationFrame(frameHandle);
      resizeObserver.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [hostRef]);

  return frame;
}

function useDotMatrixActive(hostRef: RefObject<HTMLElement | null>): boolean {
  const [visible, setVisible] = useState(true);
  const [intersecting, setIntersecting] = useState(true);

  useEffect(() => {
    if (typeof document === "undefined") return;

    const syncVisibility = () => setVisible(document.visibilityState === "visible");
    syncVisibility();
    document.addEventListener("visibilitychange", syncVisibility);
    return () => document.removeEventListener("visibilitychange", syncVisibility);
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host || typeof IntersectionObserver === "undefined") return;

    const observer = new IntersectionObserver(([entry]) => {
      setIntersecting(entry?.isIntersecting ?? false);
    });
    observer.observe(host);
    return () => observer.disconnect();
  }, [hostRef]);

  return visible && intersecting;
}

function detectRendererBackend(): DotMatrixRendererBackend {
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2", { alpha: true, antialias: false });
  if (!gl) return "none";
  if (!gl.getExtension("EXT_color_buffer_float")) return "webgl2_no_float_render_target";
  return "webgl2";
}

function errorAttrs(error: unknown): Record<string, string> {
  if (!(error instanceof Error)) {
    return {};
  }
  return {
    "error.message": error.message.slice(0, 256),
    "error.name": error.name,
  };
}
