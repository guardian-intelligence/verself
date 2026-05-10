import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { type ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";

// Letters layout — /letters and /letters/$slug share this chrome. Paper
// ground, chip Lockup, sepia rule. The layout sets data-treatment so the
// entire subtree (chrome + body + footer) resolves var(--treatment-*) to the
// Letters scope.
//
// The paper feel is composed of three fixed, viewport-tracking layers: a
// tiled fibre-noise substrate, a 28px minor / 140px major graph grid (both
// multiplied into the substrate), all uniform across the viewport so no
// region of the page reads as a different tone.

export const Route = createFileRoute("/letters")({
  component: LettersLayout,
});

function LettersLayout() {
  return (
    <div
      data-treatment="letters"
      className="relative flex min-h-svh flex-col bg-[var(--treatment-ground)] text-[var(--treatment-ink)]"
      style={{ isolation: "isolate" }}
    >
      <PaperSubstrate />
      <PaperGrid />
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

// Paper substrate. A tiled SVG fractal-noise pattern multiplied into the
// page ground so the fibre texture extends across the entire viewport,
// including under the sticky chrome and footer. Inlined as a data URL so
// it ships without an extra request and renders before idle.
const PAPER_NOISE_TILE =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' width='320' height='320'>" +
      "<filter id='p'>" +
      "<feTurbulence type='fractalNoise' baseFrequency='1.35' numOctaves='2' stitchTiles='stitch' seed='7'/>" +
      "<feColorMatrix values='0 0 0 0 0.42  0 0 0 0 0.42  0 0 0 0 0.42  0 0 0 0.5 0'/>" +
      "</filter>" +
      "<rect width='100%' height='100%' filter='url(#p)'/>" +
      "</svg>",
  );

function PaperSubstrate() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-40"
      style={{
        backgroundImage: `url("${PAPER_NOISE_TILE}")`,
        backgroundSize: "320px 320px",
        mixBlendMode: "multiply",
        opacity: 0.5,
      }}
    />
  );
}

// Graph paper grid. Two layers — 28px minor + 140px major — both multiplied
// into the substrate. A 0.25px blur on the minor layer breaks the
// glassy-perfect CSS hairline so the lines read as ink absorbed by paper
// fibres rather than overlay. The major layer (every 5 cells) gives the
// monorhythmic grid hierarchy the eye expects from real graph paper.
function PaperGrid() {
  return (
    <>
      <div
        aria-hidden
        className="pointer-events-none fixed inset-0 z-40"
        style={{
          backgroundImage:
            "linear-gradient(to right, rgba(40, 44, 52, 0.11) 1px, transparent 1px), linear-gradient(to bottom, rgba(40, 44, 52, 0.11) 1px, transparent 1px)",
          backgroundSize: "28px 28px",
          mixBlendMode: "multiply",
          filter: "blur(0.15px)",
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none fixed inset-0 z-40"
        style={{
          backgroundImage:
            "linear-gradient(to right, rgba(40, 44, 52, 0.10) 1px, transparent 1px), linear-gradient(to bottom, rgba(40, 44, 52, 0.10) 1px, transparent 1px)",
          backgroundSize: "140px 140px",
          mixBlendMode: "multiply",
        }}
      />
    </>
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
    <footer className="bg-[var(--treatment-ground)] text-[var(--treatment-ink)]">
      <div className="mx-auto w-full max-w-6xl px-4 md:px-6">
        <div className="py-10 font-mono text-[11px] leading-5 tracking-[0.01em] text-[var(--treatment-muted-faint)] sm:whitespace-nowrap">
          © 2026 Guardian Intelligence LLC · Seattle, Washington
        </div>
      </div>
    </footer>
  );
}
