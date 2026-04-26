import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";

// Letters layout — /letters and /letters/$slug share this chrome. Paper
// ground, chip Lockup, Bordeaux accent. The layout sets data-treatment so the
// entire subtree (chrome + body + footer) resolves var(--treatment-*) to the
// Letters scope, without any individual page needing to know its treatment.

export const Route = createFileRoute("/letters")({
  component: LettersLayout,
});

function LettersLayout() {
  return (
    <div
      data-treatment="letters"
      className="flex min-h-svh flex-col"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
      }}
    >
      <AppChrome
        treatment="letters"
        LinkComponent={LinkAdapter}
        slotRight={<TopNav />}
        wordmarkHref="/letters"
      />
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <LettersFooter />
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

// Letters footer — minimal colophon. Cross-treatment links live in the
// chrome's TopNav; the footer just signs the page. No top rule (the page's
// own rule above the story grid is enough), no link list.
function LettersFooter() {
  return (
    <footer
      className="mt-20"
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
