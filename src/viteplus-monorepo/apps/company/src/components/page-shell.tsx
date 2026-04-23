import { type ReactNode, useEffect } from "react";
import type { Treatment } from "@forge-metal/brand";
import { useBrandTelemetry } from "@forge-metal/brand";

// Every non-landing route uses this shell: centered column, kicker + h1
// mirroring the landing type scale, children below. The `treatment` prop
// sets data-treatment on its wrapper so the subtree reads --treatment-*
// tokens. Phase 2 supersedes the earlier {ground: "iron" | "paper"} prop —
// callers migrate either by not passing a treatment (default "company",
// same visual as the old ground="iron") or by passing the new treatment
// explicitly (treatment="letters" is the former ground="paper").
//
// The shell emits page_shell.render on mount so the ClickHouse operator
// query can correlate it with app_chrome.render on the same route.

export interface PageShellProps {
  readonly kicker: string;
  readonly heading: string;
  readonly children: ReactNode;
  readonly treatment?: Treatment;
  readonly route?: string;
}

export function PageShell({
  kicker,
  heading,
  children,
  treatment = "company",
  route,
}: PageShellProps) {
  const emitSpan = useBrandTelemetry();

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("page_shell.render", {
      route: route ?? window.location.pathname,
      treatment,
    });
  }, [treatment, route, emitSpan]);

  return (
    <div
      data-treatment={treatment}
      className="transition-colors duration-300 ease-out"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <div className="mx-auto w-full max-w-4xl px-4 py-16 md:px-6 md:py-24">
        <p
          className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          {kicker}
        </p>
        <h1
          className="mt-5 font-display"
          style={{
            fontVariationSettings: '"opsz" 96, "SOFT" 30',
            fontWeight: 400,
            fontSize: "clamp(32px, 5vw, 52px)",
            lineHeight: 1.05,
            letterSpacing: "-0.022em",
            color: "var(--treatment-ink)",
            maxWidth: "22ch",
            margin: 0,
          }}
        >
          {heading}
        </h1>
        <div className="mt-10 flex flex-col gap-5" style={{ maxWidth: "62ch" }}>
          {children}
        </div>
      </div>
    </div>
  );
}

export function BodyParagraph({ children }: { children: ReactNode }) {
  return (
    <p
      style={{
        fontFamily: "'Geist', sans-serif",
        fontWeight: 400,
        fontSize: "clamp(16px, 1.6vw, 18px)",
        lineHeight: 1.55,
        color: "var(--treatment-muted)",
        margin: 0,
      }}
    >
      {children}
    </p>
  );
}
