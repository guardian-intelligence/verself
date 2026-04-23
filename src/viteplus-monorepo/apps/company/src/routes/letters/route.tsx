import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@forge-metal/brand";

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
      <AppChrome treatment="letters" LinkComponent={LinkAdapter} />
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

// Letters footer — leaner than the Workshop footer. Paper readers expect a
// colophon-style sign-off, not a three-column site nav. One column with the
// routes that still make sense inside the editorial register; the uppercase
// tracked-mono masthead line anchors the page like a periodical.
function LettersFooter() {
  return (
    <footer
      className="mt-20"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        borderTop: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 py-10 md:px-6">
        <p
          className="font-mono text-[10px] font-medium uppercase tracking-[0.18em]"
          style={{ color: "var(--treatment-muted-faint)" }}
        >
          Read also
        </p>
        <ul className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
          <li>
            <Link
              to="/letters"
              className="transition-colors hover:underline hover:underline-offset-4"
              style={{ color: "var(--treatment-muted)" }}
            >
              Letters index
            </Link>
          </li>
          <li>
            <a
              href="/letters/rss"
              className="transition-colors hover:underline hover:underline-offset-4"
              style={{ color: "var(--treatment-muted)" }}
            >
              RSS
            </a>
          </li>
          <li>
            <Link
              to="/newsroom"
              className="transition-colors hover:underline hover:underline-offset-4"
              style={{ color: "var(--treatment-muted)" }}
            >
              Newsroom
            </Link>
          </li>
          <li>
            <Link
              to="/"
              className="transition-colors hover:underline hover:underline-offset-4"
              style={{ color: "var(--treatment-muted)" }}
            >
              Workshop
            </Link>
          </li>
        </ul>
      </div>
      <div
        className="mx-auto w-full max-w-4xl px-4 pb-10 md:px-6"
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
