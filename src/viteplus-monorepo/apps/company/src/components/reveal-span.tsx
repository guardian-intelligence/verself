"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { emitSpan } from "~/lib/telemetry/browser";

export interface RevealSpanProps {
  readonly spanName: string;
  readonly attrs: Record<string, string>;
  readonly threshold?: number;
  readonly children: ReactNode;
  readonly as?: "div" | "section" | "p";
  readonly className?: string;
  readonly style?: React.CSSProperties;
}

// Fires a single OTLP span the first time the wrapped element crosses the
// visibility threshold. Self-disconnects after emission so scrolling back and
// forth doesn't double-count. No-op during SSR; relies on IntersectionObserver
// which every evergreen browser ships.
//
// Used by the landing to emit company.landing.hero_view and
// company.landing.section_view without adding an effect to every page.
export function RevealSpan({
  spanName,
  attrs,
  threshold = 0.4,
  children,
  as = "div",
  className,
  style,
}: RevealSpanProps) {
  const ref = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const node = ref.current;
    if (!node) return;

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.intersectionRatio >= threshold) {
            emitSpan(spanName, attrs);
            observer.disconnect();
            return;
          }
        }
      },
      { threshold: [0, threshold, 1] },
    );
    observer.observe(node);
    return () => observer.disconnect();
    // attrs is a plain record — JSON.stringify gives a stable dependency
    // identity per unique attribute set without requiring referential equality
    // from the caller.
  }, [spanName, threshold, JSON.stringify(attrs)]);

  const Tag = as;
  // Ref typing has to satisfy every union member; the cast is safe because the
  // concrete element matches whichever Tag the caller chose.
  return (
    <Tag ref={ref as React.Ref<never>} className={className} style={style}>
      {children}
    </Tag>
  );
}
