import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";
import { criticalTreatmentHead, criticalTreatmentRootStyle } from "~/lib/critical-treatment";
import newsroomCriticalCss from "~/styles/critical/newsroom.css?inline";

// News layout — /news and /news/$slug share the Newsroom treatment chrome:
// Flare ground, emboss Lockup, ink type. The broadcast register is Guardian
// appearing in someone else's feed.
//
// The lockup self-targets /news (the section index) instead of /. The
// chrome's bottom rule renders only on the index — on /news/$slug the
// article header carries its own structure and the rule against Flare
// would read as redundant chrome.

export const Route = createFileRoute("/news")({
  component: NewsroomLayout,
  head: () => criticalTreatmentHead("newsroom", newsroomCriticalCss),
});

function NewsroomLayout() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isIndex = pathname === "/news" || pathname === "/news/";
  return (
    <div
      data-treatment="newsroom"
      className="flex min-h-svh flex-col"
      style={criticalTreatmentRootStyle("newsroom")}
    >
      <AppChrome
        treatment="newsroom"
        LinkComponent={LinkAdapter}
        slotRight={<TopNav />}
        wordmarkHref="/news"
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
          className="whitespace-nowrap py-10 text-[10px] tracking-[0.08em] md:text-[11px] md:tracking-[0.12em]"
          style={{
            fontFamily: "'Geist Mono', ui-monospace, monospace",
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
