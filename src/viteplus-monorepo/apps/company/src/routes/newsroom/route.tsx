import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@verself/brand";

// Newsroom layout — /newsroom and any future /newsroom/$slug share this
// chrome. Flare ground, emboss Lockup, ink type. The broadcast register:
// Guardian appearing in someone else's feed.

export const Route = createFileRoute("/newsroom")({
  component: NewsroomLayout,
});

function NewsroomLayout() {
  return (
    <div
      data-treatment="newsroom"
      className="flex min-h-svh flex-col"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <AppChrome treatment="newsroom" LinkComponent={LinkAdapter} />
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <NewsroomFooter />
    </div>
  );
}

function LinkAdapter(props: {
  to: string;
  className?: string;
  style?: React.CSSProperties;
  "aria-label"?: string;
  onClick?: React.MouseEventHandler;
  children?: ReactNode;
}) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return <Link {...(props as any)} />;
}

// Newsroom footer — minimal. Flare is broadcast, not navigation; a loud
// ground asking the reader to scan nine links is fatiguing. A single masthead
// line and two escape hatches (back to Workshop, over to Letters).
function NewsroomFooter() {
  return (
    <footer
      className="mt-16"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        borderTop: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto flex w-full max-w-7xl flex-wrap items-baseline gap-x-6 gap-y-2 px-4 py-8 md:px-6">
        <p
          className="font-mono text-[11px] font-semibold uppercase tracking-[0.2em]"
          style={{ color: "var(--treatment-muted)", fontVariationSettings: '"wght" 600' }}
        >
          Newsroom · Guardian Intelligence
        </p>
        <Link
          to="/"
          className="font-mono text-[11px] uppercase tracking-[0.16em] transition-colors hover:underline hover:underline-offset-4"
          style={{ color: "var(--treatment-muted-meta)" }}
        >
          ← Back to Workshop
        </Link>
        <Link
          to="/letters"
          className="font-mono text-[11px] uppercase tracking-[0.16em] transition-colors hover:underline hover:underline-offset-4"
          style={{ color: "var(--treatment-muted-meta)" }}
        >
          Letters →
        </Link>
      </div>
      <div
        className="mx-auto w-full max-w-7xl px-4 pb-8 md:px-6"
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontSize: "11px",
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "var(--treatment-muted-faint)",
        }}
      >
        © 2026 Guardian Intelligence LLC · Seattle, Washington
      </div>
    </footer>
  );
}
