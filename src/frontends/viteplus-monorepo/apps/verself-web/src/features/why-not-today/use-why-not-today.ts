import { useEffect, useLayoutEffect, useState, type RefObject } from "react";
import type { DegradedReason, RendererBackend, SceneFrame } from "./types";

type RuntimeState =
  | { readonly kind: "pending" }
  | { readonly kind: "ready"; readonly backend: RendererBackend }
  | {
      readonly kind: "fallback";
      readonly backend: RendererBackend;
      readonly reason: DegradedReason;
    };

export function useSceneRuntime(motion: boolean | undefined): RuntimeState {
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
      if (reducedMotion) {
        setState({ kind: "fallback", backend, reason: "reduced_motion" });
        return;
      }
      if (backend === "none") {
        setState({ kind: "fallback", backend, reason: "no_renderer" });
        return;
      }
      setState({ kind: "ready", backend });
    };
    if (document.readyState === "complete") {
      const id = window.requestAnimationFrame(start);
      return () => {
        cancelled = true;
        window.cancelAnimationFrame(id);
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

export function useSceneFrame(hostRef: RefObject<HTMLElement | null>): SceneFrame | undefined {
  const [frame, setFrame] = useState<SceneFrame>();
  useLayoutEffect(() => {
    if (typeof window === "undefined") return;
    const update = () => {
      const host = hostRef.current;
      if (!host) return;
      const box = host.getBoundingClientRect();
      if (box.width <= 0 || box.height <= 0) return;
      setFrame({
        viewport: { w: box.width, h: box.height, dpr: window.devicePixelRatio || 1 },
      });
    };
    const id = window.requestAnimationFrame(update);
    const ro = new ResizeObserver(update);
    const host = hostRef.current;
    if (host) ro.observe(host);
    window.addEventListener("resize", update);
    return () => {
      window.cancelAnimationFrame(id);
      ro.disconnect();
      window.removeEventListener("resize", update);
    };
  }, [hostRef]);
  return frame;
}

export function useSceneActive(hostRef: RefObject<HTMLElement | null>): boolean {
  const [visible, setVisible] = useState(true);
  const [intersecting, setIntersecting] = useState(true);
  useEffect(() => {
    if (typeof document === "undefined") return;
    const sync = () => setVisible(document.visibilityState === "visible");
    sync();
    document.addEventListener("visibilitychange", sync);
    return () => document.removeEventListener("visibilitychange", sync);
  }, []);
  useEffect(() => {
    const host = hostRef.current;
    if (!host || typeof IntersectionObserver === "undefined") return;
    const obs = new IntersectionObserver(([entry]) => {
      setIntersecting(entry?.isIntersecting ?? false);
    });
    obs.observe(host);
    return () => obs.disconnect();
  }, [hostRef]);
  return visible && intersecting;
}

function detectRendererBackend(): RendererBackend {
  const canvas = document.createElement("canvas");
  const gl = canvas.getContext("webgl2", { alpha: true, antialias: true });
  return gl ? "webgl2" : "none";
}
