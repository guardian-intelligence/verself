import { createFileRoute, Link, Outlet, useParams } from "@tanstack/react-router";
import { type CSSProperties, type ReactNode } from "react";
import { AppChrome } from "@verself/brand";
import { TopNav } from "~/components/top-nav";

// Letters layout — /letters and /letters/$slug share this chrome. Paper
// ground, chip Lockup, sepia rule. The layout sets data-treatment so the
// entire subtree (chrome + body + footer) resolves var(--treatment-*) to the
// Letters scope.
//
// The graph paper is one seeded procedural relief — feTurbulence (the
// browser's native fractal-noise primitive), seed = a hash of the slug, so
// every letter is its own sheet, deterministic across loads and SSR/hydration
// (never Math.random). There is exactly ONE noise field ("the hills"). The
// zones below are NOT separate noise — they are fixed geometry that remaps
// the same hills into a different opacity band:
//
//   • reading column (centred max-w-6xl) ........ 5–20%   (quiet, readable)
//   • page margins (outside that column) ........ 30–40%  (gentle bloom)
//   • top strip (navbar + 350px below it) ....... 0–1%    (masthead band;
//                                                          near-blank,
//                                                          overrides the
//                                                          others there)
//
// Zone hand-offs are soft ramps, not hard edges. Because every zone reads
// the same field, the relief is continuous — one page whose contour
// intensity is attenuated over the text and under the masthead. On phones
// the column is ~full width, so there are no margins and the sheet sits in
// the quiet band — correct for reading.
//
// The layers are position:absolute over the document (NOT viewport fixed)
// and multiply over the ink, so the words read as printed onto this exact
// sheet and it scrolls 1:1 with them. Text is never touched: no blend, no
// clip, no JS — it stays selectable/screen-reader-clean. The paper is
// CSS/SVG only (feTurbulence = the procedural model without a WebGL context).

export const Route = createFileRoute("/letters")({
  component: LettersLayout,
});

// FNV-1a, 32-bit — a pure function of the slug. Math.imul keeps it a true
// 32-bit multiply across engines. Reduced into feTurbulence's integer seed
// space; distinct slugs land on distinct sheets.
function slugSeed(slug: string): number {
  let h = 2166136261 >>> 0;
  for (let i = 0; i < slug.length; i++) {
    h ^= slug.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0) % 65535;
}

// One self-contained sheet: a 2800px tile (2800 = 28×100 = 140×20, so the
// minor and major ruling align perfectly across tile seams) holding the
// 28/140px grid, its alpha multiplied by a seeded fractal-noise relief
// linearly remapped into [lo, hi]. baseFrequency 0.0011 → the average
// "hill" is ~2× the size it was at 0.0022; the larger 2800 tile keeps the
// repeat period well past a screen so the bigger features don't telegraph.
function gridReliefDataUri(seed: number, lo: number, hi: number): string {
  const slope = (hi - lo).toFixed(4);
  const intercept = lo.toFixed(4);
  const svg =
    "<svg xmlns='http://www.w3.org/2000/svg' width='2800' height='2800'>" +
    "<defs>" +
    "<pattern id='g' width='28' height='28' patternUnits='userSpaceOnUse'>" +
    "<path d='M28 0V28M0 28H28' stroke='rgb(40,44,52)' stroke-width='1' fill='none' shape-rendering='crispEdges'/>" +
    "</pattern>" +
    "<pattern id='G' width='140' height='140' patternUnits='userSpaceOnUse'>" +
    "<path d='M140 0V140M0 140H140' stroke='rgb(40,44,52)' stroke-width='1' fill='none' shape-rendering='crispEdges'/>" +
    "</pattern>" +
    "<filter id='r' x='0' y='0' width='100%' height='100%' color-interpolation-filters='sRGB'>" +
    `<feTurbulence type='fractalNoise' baseFrequency='0.0011' numOctaves='3' seed='${seed}' stitchTiles='stitch' result='n'/>` +
    "<feColorMatrix in='n' type='matrix' values='0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  1 0 0 0 0' result='na'/>" +
    `<feComponentTransfer in='na' result='nb'><feFuncA type='linear' slope='${slope}' intercept='${intercept}'/></feComponentTransfer>` +
    "<feComposite in='SourceGraphic' in2='nb' operator='in'/>" +
    "</filter>" +
    "</defs>" +
    "<g filter='url(#r)'>" +
    "<rect width='2800' height='2800' fill='url(#g)'/>" +
    "<rect width='2800' height='2800' fill='url(#G)'/>" +
    "</g>" +
    "</svg>";
  return "data:image/svg+xml;utf8," + encodeURIComponent(svg);
}

// Fine fibre tile — the paper's tooth, and the dither that keeps the
// remapped relief free of 8-bit banding. Static, uniform, fixed SVG seed.
const PAPER_FIBRE_TILE =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    "<svg xmlns='http://www.w3.org/2000/svg' width='320' height='320'>" +
      "<filter id='f'>" +
      "<feTurbulence type='fractalNoise' baseFrequency='1.35' numOctaves='2' stitchTiles='stitch' seed='7'/>" +
      "<feColorMatrix values='0 0 0 0 0.42  0 0 0 0 0.42  0 0 0 0 0.42  0 0 0 0.42 0'/>" +
      "</filter>" +
      "<rect width='100%' height='100%' filter='url(#f)'/>" +
      "</svg>",
  );

// Absolute, not fixed: layer is the size of the whole document and scrolls
// with it. No fade-in — the paper paints with the page.
const PAPER_LAYER_CLASS = "pointer-events-none absolute inset-0 z-40";

// Geometry (deterministic, not seeded):
//  --lp-col   half of max-w-6xl (72rem ≈ 1152px) — the reading column edge.
//  --lp-ramp  soft horizontal hand-off across the column edge. Below a
//             ~1152px viewport the calc()s cross over and the margin band
//             collapses to nothing → the whole sheet is the quiet band
//             (correct on phones).
//  --lp-strip navbar height + 350px — the near-blank top region. Anchored
//             to the document top (the sheet scrolls 1:1, so this is the
//             first ~406px of the page, not a viewport-pinned band).
//  --lp-tramp soft vertical hand-off at the strip's lower edge.
const PAPER_GEOMETRY_VARS = {
  ["--lp-col" as string]: "576px",
  ["--lp-ramp" as string]: "110px",
  ["--lp-strip" as string]: "calc(var(--header-h) + 350px)",
  ["--lp-tramp" as string]: "70px",
} as CSSProperties;

const TEXT_ZONE_MASK =
  "linear-gradient(to right," +
  " transparent 0," +
  " transparent calc(50% - var(--lp-col) - var(--lp-ramp))," +
  " #000 calc(50% - var(--lp-col) + var(--lp-ramp))," +
  " #000 calc(50% + var(--lp-col) - var(--lp-ramp))," +
  " transparent calc(50% + var(--lp-col) + var(--lp-ramp))," +
  " transparent 100%)";

const MARGIN_ZONE_MASK =
  "linear-gradient(to right," +
  " #000 0," +
  " #000 calc(50% - var(--lp-col) - var(--lp-ramp))," +
  " transparent calc(50% - var(--lp-col) + var(--lp-ramp))," +
  " transparent calc(50% + var(--lp-col) - var(--lp-ramp))," +
  " #000 calc(50% + var(--lp-col) + var(--lp-ramp))," +
  " #000 100%)";

// Everything below the top strip (zone layers live here).
const BELOW_STRIP_MASK =
  "linear-gradient(to bottom," +
  " transparent 0," +
  " transparent calc(var(--lp-strip) - var(--lp-tramp))," +
  " #000 calc(var(--lp-strip) + var(--lp-tramp))," +
  " #000 100%)";

// The top strip itself (the calm 0–15% region).
const TOP_STRIP_MASK =
  "linear-gradient(to bottom," +
  " #000 0," +
  " #000 calc(var(--lp-strip) - var(--lp-tramp))," +
  " transparent calc(var(--lp-strip) + var(--lp-tramp))," +
  " transparent 100%)";

function LettersLayout() {
  // strict:false so the layout can read the child route's slug; on the
  // index there is no slug, so the sheet falls back to a stable "letters".
  const params = useParams({ strict: false }) as { slug?: string };
  const seed = slugSeed(params.slug ?? "letters");

  // One field, three bands. Same seed/frequency/coords → continuous relief;
  // only the output range differs by zone.
  const textBand = gridReliefDataUri(seed, 0.05, 0.2);
  const marginBand = gridReliefDataUri(seed, 0.3, 0.4);
  const topBand = gridReliefDataUri(seed, 0.0, 0.01);

  return (
    <div
      data-treatment="letters"
      className="relative flex min-h-svh flex-col bg-[var(--treatment-ground)] text-[var(--treatment-ink)]"
      style={{ isolation: "isolate", ...PAPER_GEOMETRY_VARS }}
    >
      <PaperGrain />
      {/* Zone layers are clipped to below the top strip, so the strip is
          owned solely by the 0–15% layer. */}
      <GridLayer image={textBand} masks={[TEXT_ZONE_MASK, BELOW_STRIP_MASK]} />
      <GridLayer image={marginBand} masks={[MARGIN_ZONE_MASK, BELOW_STRIP_MASK]} />
      <GridLayer image={topBand} masks={[TOP_STRIP_MASK]} />
      <div className="relative z-10 flex flex-1 flex-col">
        <AppChrome
          treatment="letters"
          LinkComponent={LinkAdapter}
          slotRight={<TopNav />}
          wordmarkHref="/letters"
          sticky={false}
        />
        <main id="main" className="flex-1">
          <Outlet />
        </main>
        <LettersFooter />
      </div>
    </div>
  );
}

function PaperGrain() {
  return (
    <div
      aria-hidden
      className={PAPER_LAYER_CLASS}
      style={{
        backgroundImage: `url("${PAPER_FIBRE_TILE}")`,
        backgroundSize: "320px 320px",
        mixBlendMode: "multiply",
        opacity: 0.34,
      }}
    />
  );
}

// One band of the sheet: the seeded relief-modulated grid, gated to its
// zone(s). One mask → applied directly; two masks → intersected (shown only
// where BOTH are opaque), so a horizontal zone can be clipped to below the
// top strip without a second DOM layer. -webkit-mask-composite:source-in is
// the legacy WebKit spelling of the standard mask-composite:intersect. The
// 2800px relief tile repeats down the whole scrolling document.
function GridLayer({ image, masks }: { image: string; masks: readonly string[] }) {
  const joined = masks.join(", ");
  const repeat = masks.map(() => "no-repeat").join(", ");
  const size = masks.map(() => "100% 100%").join(", ");
  const composite = masks.length > 1 ? { maskComposite: "intersect" } : {};
  const webkitComposite = masks.length > 1 ? { WebkitMaskComposite: "source-in" } : {};
  return (
    <div
      aria-hidden
      className={PAPER_LAYER_CLASS}
      style={
        {
          backgroundImage: `url("${image}")`,
          backgroundSize: "2800px 2800px",
          backgroundRepeat: "repeat",
          mixBlendMode: "multiply",
          WebkitMaskImage: joined,
          maskImage: joined,
          WebkitMaskRepeat: repeat,
          maskRepeat: repeat,
          WebkitMaskSize: size,
          maskSize: size,
          ...webkitComposite,
          ...composite,
        } as CSSProperties
      }
    />
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
