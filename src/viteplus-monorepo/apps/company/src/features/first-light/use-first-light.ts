"use client";

import { useEffect, useLayoutEffect, useState, type RefObject } from "react";
import { emitSpan } from "~/lib/telemetry/browser";
import { FIRST_LIGHT_TOTAL_MS, trailLuminance } from "./shader/envelopes";
import type { DegradedReason, FirstLightGeometry, FirstLightRect, RendererBackend } from "./types";

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

export function useFirstLightGeometry(
  hostRef: RefObject<HTMLElement | null>,
  trailTargetRef: RefObject<HTMLElement | null>,
  wingsAnchorRef: RefObject<HTMLElement | null>,
): FirstLightGeometry | undefined {
  const [geometry, setGeometry] = useState<FirstLightGeometry>();

  useLayoutEffect(() => {
    if (typeof window === "undefined") return;

    const update = () => {
      const host = hostRef.current;
      const trail = trailTargetRef.current;
      const wings = wingsAnchorRef.current;
      if (!host || !trail || !wings) return;

      const hostBox = host.getBoundingClientRect();
      if (hostBox.width <= 0 || hostBox.height <= 0) return;

      setGeometry({
        viewport: {
          w: hostBox.width,
          h: hostBox.height,
          dpr: window.devicePixelRatio || 1,
        },
        trail: rectWithin(hostBox, trail.getBoundingClientRect()),
        wings: rectWithin(hostBox, wings.getBoundingClientRect()),
      });
    };

    const frame = window.requestAnimationFrame(update);
    const resizeObserver = new ResizeObserver(update);
    const host = hostRef.current;
    const trail = trailTargetRef.current;
    const wings = wingsAnchorRef.current;
    if (host) resizeObserver.observe(host);
    if (trail) resizeObserver.observe(trail);
    if (wings) resizeObserver.observe(wings);

    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, { passive: true });
    return () => {
      window.cancelAnimationFrame(frame);
      resizeObserver.disconnect();
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update);
    };
  }, [hostRef, trailTargetRef, wingsAnchorRef]);

  return geometry;
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

export function useTrailLuminance(
  trailTargetRef: RefObject<HTMLElement | null>,
  enabled: boolean,
): void {
  useEffect(() => {
    const target = trailTargetRef.current;
    if (!enabled || !target || typeof window === "undefined") return;

    const started = performance.now();
    let frame = 0;
    const tick = (now: number) => {
      const elapsed = now - started;
      target.style.setProperty("--firstlight-luminance", trailLuminance(elapsed).toFixed(3));
      if (elapsed <= FIRST_LIGHT_TOTAL_MS) {
        frame = window.requestAnimationFrame(tick);
      }
    };

    frame = window.requestAnimationFrame(tick);
    return () => {
      window.cancelAnimationFrame(frame);
      target.style.removeProperty("--firstlight-luminance");
    };
  }, [enabled, trailTargetRef]);
}

function detectRendererBackend(): RendererBackend {
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2", { alpha: true, antialias: false });
  return gl ? "webgl2" : "none";
}

function rectWithin(host: DOMRect, rect: DOMRect): FirstLightRect {
  return {
    x: (rect.left - host.left) / host.width,
    y: (rect.top - host.top) / host.height,
    w: rect.width / host.width,
    h: rect.height / host.height,
  };
}
