"use client";

import { useEffect, useLayoutEffect, useState, type RefObject } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import type { DegradedReason, FirstLightFrame, RendererBackend } from "./types";

type RuntimeState =
  | { readonly kind: "pending" }
  | { readonly kind: "ready"; readonly backend: RendererBackend }
  | {
      readonly kind: "fallback";
      readonly backend: RendererBackend;
      readonly reason: DegradedReason;
      readonly reducedMotion: boolean;
    };

export function useFirstLightRuntime(motion: boolean | undefined): RuntimeState {
  const [state, setState] = useState<RuntimeState>({ kind: "pending" });

  useEffect(() => {
    if (typeof window === "undefined") return;

    let cancelled = false;
    const start = () => {
      if (cancelled) return;

      const reducedMotion =
        motion === false ||
        (motion !== true && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
      const backend = reducedMotion ? "none" : detectRendererBackend();
      emitSpan("company.first_light.capability", {
        renderer_backend: backend,
        prefers_reduced_motion: String(reducedMotion),
        device_pixel_ratio: String(window.devicePixelRatio || 1),
        "viewport.w": String(window.innerWidth),
        "viewport.h": String(window.innerHeight),
      });

      if (reducedMotion) {
        emitSpan("company.first_light.degraded", { reason: "reduced_motion" });
        setState({ kind: "fallback", backend, reason: "reduced_motion", reducedMotion });
        return;
      }
      if (backend === "none") {
        emitSpan("company.first_light.degraded", { reason: "no_renderer" });
        setState({ kind: "fallback", backend, reason: "no_renderer", reducedMotion });
        return;
      }
      setState({ kind: "ready", backend });
    };

    if (document.readyState === "complete") {
      const frame = window.requestAnimationFrame(start);
      return () => {
        cancelled = true;
        window.cancelAnimationFrame(frame);
      };
    }

    window.addEventListener("load", start, { once: true });
    return () => {
      cancelled = true;
      window.removeEventListener("load", start);
    };
  }, [motion]);

  return state;
}

export function useFirstLightFrame(
  hostRef: RefObject<HTMLElement | null>,
): FirstLightFrame | undefined {
  const [frame, setFrame] = useState<FirstLightFrame>();

  useLayoutEffect(() => {
    if (typeof window === "undefined") return;

    const update = () => {
      const host = hostRef.current;
      if (!host) return;

      const hostBox = host.getBoundingClientRect();
      if (hostBox.width <= 0 || hostBox.height <= 0) return;

      setFrame({
        viewport: {
          w: hostBox.width,
          h: hostBox.height,
          dpr: window.devicePixelRatio || 1,
        },
      });
    };

    const frame = window.requestAnimationFrame(update);
    const resizeObserver = new ResizeObserver(update);
    const host = hostRef.current;
    if (host) resizeObserver.observe(host);

    window.addEventListener("resize", update);
    return () => {
      window.cancelAnimationFrame(frame);
      resizeObserver.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [hostRef]);

  return frame;
}

export function useFirstLightActive(hostRef: RefObject<HTMLElement | null>): boolean {
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

function detectRendererBackend(): RendererBackend {
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2", { alpha: true, antialias: false });
  return gl ? "webgl2" : "none";
}
