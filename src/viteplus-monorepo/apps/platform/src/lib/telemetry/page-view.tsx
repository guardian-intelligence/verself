"use client";

import { useEffect, useRef } from "react";
import { useRouterState } from "@tanstack/react-router";
import { onCLS, onINP, onLCP, type Metric } from "web-vitals";
import { emitSpan, initBrowserTelemetry } from "./browser";

let webVitalsInstalled = false;

function installWebVitals(): void {
  if (webVitalsInstalled || typeof window === "undefined") return;
  webVitalsInstalled = true;

  const handler = (kind: "lcp" | "cls" | "inp") => (metric: Metric) => {
    emitSpan(`web_vital.${kind}`, {
      "web_vital.id": metric.id,
      "web_vital.name": metric.name,
      "web_vital.rating": metric.rating,
      "web_vital.value": String(metric.value),
      "web_vital.delta": String(metric.delta),
      "web_vital.navigation_type": metric.navigationType,
      "route.path": window.location.pathname,
    });
  };

  onLCP(handler("lcp"));
  onCLS(handler("cls"));
  onINP(handler("inp"));
}

// Mounted at the root and emits a `page_view` span on initial load and on
// every subsequent route resolution. Web Vitals callbacks are wired once and
// fire whenever the browser reports a new measurement (not per route).
export function TelemetryProbe() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const previousPath = useRef<string | undefined>(undefined);

  useEffect(() => {
    initBrowserTelemetry();
    installWebVitals();
  }, []);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const navigation: PerformanceNavigationTiming | undefined = performance.getEntriesByType(
      "navigation",
    )[0] as PerformanceNavigationTiming | undefined;
    emitSpan("page_view", {
      "route.path": path,
      "route.previous_path": previousPath.current ?? "",
      referrer: document.referrer,
      "navigation.type": navigation?.type ?? "unknown",
    });
    previousPath.current = path;
  }, [path]);

  return null;
}
