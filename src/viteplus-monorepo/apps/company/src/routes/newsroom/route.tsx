import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";

// Newsroom layout — /newsroom and any future /newsroom/$slug share this
// chrome. Flare ground, emboss Lockup, ink type. The broadcast register:
// Guardian appearing in someone else's feed.
//
// The lockup self-targets /newsroom (the section index) instead of /. The
// chrome's bottom rule renders only on the index — on /newsroom/$slug the
// article header carries its own structure and the rule against Flare
// would read as redundant chrome.

export const Route = createFileRoute("/newsroom")({
  component: NewsroomLayout,
});

function NewsroomLayout() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isIndex = pathname === "/newsroom" || pathname === "/newsroom/";
  return (
    <div
      data-treatment="newsroom"
      className="flex min-h-svh flex-col"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <AppChrome
        treatment="newsroom"
        LinkComponent={LinkAdapter}
        slotRight={<TopNav />}
        wordmarkHref="/newsroom"
        bottomRule={isIndex}
      />
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

// Newsroom footer — minimal colophon. Cross-treatment links live in the
// chrome's TopNav; the footer just signs the page. No top rule, no link
// list — same shape as LettersFooter.
function NewsroomFooter() {
  return (
    <footer
      className="mt-16"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <div className="mx-auto w-full max-w-6xl px-4 md:px-6">
        <div
          className="py-10"
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
      </div>
    </footer>
  );
}
