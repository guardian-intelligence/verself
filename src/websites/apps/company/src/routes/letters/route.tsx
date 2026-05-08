import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { useEffect, useState, type ComponentType, type ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";

// Letters layout — /letters and /letters/$slug share this chrome. Paper
// ground, chip Lockup, sepia rule. The layout sets data-treatment so the
// entire subtree (chrome + body + footer) resolves var(--treatment-*) to the
// Letters scope.
//
// Two backdrop effects sit behind the content: a CSS graph paper grid that
// renders immediately, and a film grain overlay that lazy-loads on idle
// and fades in. Both are aria-hidden, pointer-events: none, fixed to the
// viewport so they don't fight with text scroll.

export const Route = createFileRoute("/letters")({
  component: LettersLayout,
});

function LettersLayout() {
  return (
    <div
      data-treatment="letters"
      className="relative flex min-h-svh flex-col bg-[var(--treatment-ground)] text-[var(--treatment-ink)]"
    >
      <GraphPaper />
      <LazyFilmGrain />
      <div className="relative z-10 flex flex-1 flex-col">
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
    </div>
  );
}

// Graph paper backdrop. Two crossed linear-gradients on a sepia-tinted
// hairline; cell size is a single value tuned to read as "graph paper" on
// both phone and desktop without becoming a precision grid. The opacity is
// low enough that body type still wins the page.
function GraphPaper() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-0"
      style={{
        backgroundImage:
          "linear-gradient(to right, rgba(91, 58, 38, 0.05) 1px, transparent 1px), linear-gradient(to bottom, rgba(91, 58, 38, 0.05) 1px, transparent 1px)",
        backgroundSize: "28px 28px",
      }}
    />
  );
}

// Idle-loaded film grain. The dynamic import is fired from
// requestIdleCallback so the grain chunk never blocks the initial paint,
// and the loaded component fades itself in. Falls back to a deferred
// setTimeout in browsers without requestIdleCallback.
function LazyFilmGrain() {
  const [Grain, setGrain] = useState<ComponentType | null>(null);
  useEffect(() => {
    const w = window as Window &
      typeof globalThis & {
        requestIdleCallback?: (cb: () => void) => number;
        cancelIdleCallback?: (handle: number) => void;
      };
    const schedule = w.requestIdleCallback ?? ((cb: () => void) => window.setTimeout(cb, 200));
    const cancel = w.cancelIdleCallback ?? ((handle: number) => window.clearTimeout(handle));
    const handle = schedule(() => {
      void import("./letters-grain").then((mod) => setGrain(() => mod.LettersGrain));
    });
    return () => cancel(handle);
  }, []);
  return Grain ? <Grain /> : null;
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
    <footer className="bg-[var(--treatment-ground)] text-[var(--treatment-ink)]">
      <div className="mx-auto w-full max-w-6xl px-4 md:px-6">
        <div className="py-10 font-mono text-[11px] leading-5 tracking-[0.01em] text-[var(--treatment-muted-faint)] sm:whitespace-nowrap">
          © 2026 Guardian Intelligence LLC · Seattle, Washington
        </div>
      </div>
    </footer>
  );
}
