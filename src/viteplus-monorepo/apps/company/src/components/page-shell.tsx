import { type ReactNode, useEffect } from "react";
import { useBrandTelemetry } from "@verself/brand";

// PageShell — the canonical non-landing surface: centered column, kicker +
// h1 matching the landing type scale, children below. Treatment is inherited
// from the ancestor layout root (_workshop, letters, newsroom) via
// data-treatment — PageShell does not redeclare a scope, so the same shell
// renders the Workshop register on /company and the Letters register on
// /letters, automatically.
//
// Emits page_shell.render on mount so ClickHouse can correlate it with the
// layout's app_chrome.render span on the same trace.

export interface PageShellProps {
  readonly kicker: string;
  readonly heading: string;
  readonly children: ReactNode;
  readonly route?: string;
}

export function PageShell({ kicker, heading, children, route }: PageShellProps) {
  const emitSpan = useBrandTelemetry();

  useEffect(() => {
    if (typeof window === "undefined") return;
    emitSpan("page_shell.render", {
      route: route ?? window.location.pathname,
    });
  }, [route, emitSpan]);

  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-16 md:px-6 md:py-24">
      <p
        className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
        style={{ color: "var(--treatment-muted-faint)" }}
      >
        {kicker}
      </p>
      <h1
        className="mt-5"
        style={{
          // Treatment-driven display font — Geist under Workshop (where the
          // memory bans Fraunces), Fraunces under Letters. The opsz/SOFT axes
          // are a no-op on Geist and a refinement on Fraunces, so specifying
          // both is safe.
          fontFamily: "var(--treatment-display-font)",
          fontVariationSettings: '"opsz" 96, "SOFT" 30',
          fontWeight: 500,
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
  );
}

export function BodyParagraph({ children }: { children: ReactNode }) {
  return (
    <p
      style={{
        // Treatment-scoped body face. Workshop and Newsroom resolve to Geist
        // (Fraunces is display-only on both); Letters resolves to Fraunces so
        // long-form prose reads like a printed page, not documentation.
        fontFamily: "var(--treatment-body-font)",
        // Axes are a no-op on Geist; on Fraunces they tune the body optical
        // size and open the counters slightly for long-form legibility.
        fontVariationSettings: '"opsz" 18, "SOFT" 0',
        fontWeight: 400,
        fontSize: "clamp(17px, 1.7vw, 19px)",
        lineHeight: 1.55,
        color: "var(--treatment-muted-strong)",
        margin: 0,
      }}
    >
      {children}
    </p>
  );
}
