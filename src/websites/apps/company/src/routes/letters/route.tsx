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
//   • reading column (centred max-w-6xl) ........ 5–20%  (quiet, readable)
//   • page margins (outside that column) ........ 30–40% (gentle bloom)
//
// The calm masthead region is the area ABOVE a seeded, single-valued,
// circuitous boundary y = f(x), built by function composition:
//
//     boundary = clamp ∘ ( base ⊕ envelope · Σ sineₖ ∘ warp )
//
// base is the straight line between two seeded endpoints (left/right
// Y ∈ [50,300] px, ≥100 px apart — both true by construction); the envelope
// sin(πt) pins those endpoints exactly; the summed sine octaves give the
// wander; the domain warp makes that wander uneven. Every parameter is a
// pure hash read keyed by a label (no mutable RNG), so the edge is
// deterministic per slug. It is applied as a clip-path polygon (x in %,
// y in px so it never stretches with the document; the bottom edge is
// 100% so the grid stays solid to the page end) — no mask-composite. Above
// the curve the zone grid is simply clipped away, leaving blank paper, so
// the masthead band is calm by construction.
//
// The layers are position:absolute over the document (NOT viewport fixed)
// and multiply over the ink, so the words read as printed onto this exact
// sheet and it scrolls 1:1 with them. Text is never touched: no blend, no
// clip, no JS — it stays selectable/screen-reader-clean. The paper is
// CSS/SVG only (feTurbulence = the procedural model without a WebGL context).

export const Route = createFileRoute("/letters")({
  component: LettersLayout,
});

// FNV-1a, 32-bit, of an arbitrary string. Math.imul keeps it a true 32-bit
// multiply across engines.
function fnv1a(s: string): number {
  let h = 2166136261 >>> 0;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

// feTurbulence's integer seed space.
const slugSeed = (slug: string): number => fnv1a(slug) % 65535;

// pick: slug → label → Unit ∈ [0,1). A pure hash read, NOT a stateful RNG —
// every rule pulls only the entropy it names, order-independent and
// referentially transparent (identical on every load and SSR/hydration).
const pick = (slug: string, label: string): number => fnv1a(`${slug}:${label}`) / 4294967296;

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

// --- The seeded circuitous top boundary ---------------------------------
//
// All px. Endpoints live in [Y_MIN, Y_MAX] and are forced ≥ Y_GAP apart;
// the whole curve is clamped to [Y_FLOOR, Y_CEIL] so the wander stays
// bounded.
const Y_MIN = 50;
const Y_MAX = 300;
const Y_GAP = 100;
const Y_FLOOR = 40;
const Y_CEIL = 330;
const SAMPLES = 56;

// endpoints: the ≥100px gap is true by construction. Sample the left Y
// anywhere in [50,300]; sample the right Y across the *feasible set*
// [50,300] \ (lY−100, lY+100) by laying its low band [50, lY−100] and high
// band [lY+100, 300] end to end and mapping one Unit across their combined
// length. No rejection loop; the constraint cannot be violated.
function endpoints(slug: string): { lY: number; rY: number } {
  const lY = Y_MIN + (Y_MAX - Y_MIN) * pick(slug, "edge.l");
  const lowEnd = Math.min(Y_MAX, Math.max(Y_MIN, lY - Y_GAP));
  const highStart = Math.min(Y_MAX, Math.max(Y_MIN, lY + Y_GAP));
  const lowLen = Math.max(0, lowEnd - Y_MIN);
  const highLen = Math.max(0, Y_MAX - highStart);
  const u = pick(slug, "edge.r") * (lowLen + highLen);
  const rY = u < lowLen ? Y_MIN + u : highStart + (u - lowLen);
  return { lY, rY };
}

const TAU = Math.PI * 2;
const clamp = (y: number) => Math.min(Y_CEIL, Math.max(Y_FLOOR, y));

// boundary : t∈[0,1] → y. Pure composition. The envelope is zero at both
// ends so f(0)=lY and f(1)=rY exactly (endpoint rule survives the wander);
// the domain warp reads the octaves at a seeded-skewed parameter so the
// meander is uneven rather than a tidy wave.
function makeBoundary(slug: string): (t: number) => number {
  const { lY, rY } = endpoints(slug);
  const base = (t: number) => lY + (rY - lY) * t;
  const envelope = (t: number) => Math.sin(Math.PI * t);
  const warpAmp = 0.1 + 0.12 * pick(slug, "edge.warpAmp");
  const warpPhase = TAU * pick(slug, "edge.warpPhase");
  const warp = (t: number) => t + warpAmp * Math.sin(TAU * (0.8 * t) + warpPhase);
  const octaveSpec: ReadonlyArray<readonly [baseAmp: number, freq: number]> = [
    [40, 1.3],
    [18, 2.7],
    [9, 5.1],
  ];
  const octaves = octaveSpec.map(([baseAmp, freq], k) => ({
    amp: baseAmp * (0.55 + 0.9 * pick(slug, `edge.amp${k}`)),
    freq,
    phase: TAU * pick(slug, `edge.phase${k}`),
  }));
  const wiggle = (u: number) =>
    octaves.reduce((sum, o) => sum + o.amp * Math.sin(TAU * o.freq * u + o.phase), 0);
  return (t: number) => clamp(base(t) + envelope(t) * wiggle(warp(t)));
}

// The clip region kept on the zone layers: everything BELOW the seeded
// curve. x as % (responsive width), y as px (no vertical stretch); the
// 100% rows take the polygon to the true document bottom so the grid is
// solid for the whole scroll. Above the curve falls outside the polygon →
// clipped away → blank calm paper.
function belowCurveClip(slug: string): string {
  const f = makeBoundary(slug);
  const curve = Array.from({ length: SAMPLES }, (_, i) => {
    const t = i / (SAMPLES - 1);
    return `${(t * 100).toFixed(3)}% ${f(t).toFixed(2)}px`;
  }).join(", ");
  return `polygon(${curve}, 100% 100%, 0% 100%)`;
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

// Half of max-w-6xl (72rem ≈ 1152px) — the centred reading column's edge —
// and the soft horizontal hand-off across it. Below a ~1152px viewport the
// calc()s cross over and the margin band collapses to nothing → the whole
// sheet is the quiet band (correct on phones).
const PAPER_GEOMETRY_VARS = {
  ["--lp-col" as string]: "576px",
  ["--lp-ramp" as string]: "110px",
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

function LettersLayout() {
  // strict:false so the layout can read the child route's slug; on the
  // index there is no slug, so the sheet falls back to a stable "letters".
  const params = useParams({ strict: false }) as { slug?: string };
  const slug = params.slug ?? "letters";
  const seed = slugSeed(slug);

  // One field, two bands. Same seed/frequency/coords → continuous relief;
  // only the output range differs by zone.
  const textBand = gridReliefDataUri(seed, 0.05, 0.2);
  const marginBand = gridReliefDataUri(seed, 0.3, 0.4);

  // The seeded boundary, as a clip-path kept on both zone layers.
  const clipPath = belowCurveClip(slug);

  return (
    <div
      data-treatment="letters"
      className="relative flex min-h-svh flex-col bg-[var(--treatment-ground)] text-[var(--treatment-ink)]"
      style={{ isolation: "isolate", ...PAPER_GEOMETRY_VARS }}
    >
      <PaperGrain />
      <GridLayer image={textBand} zoneMask={TEXT_ZONE_MASK} clipPath={clipPath} />
      <GridLayer image={marginBand} zoneMask={MARGIN_ZONE_MASK} clipPath={clipPath} />
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

// One band of the sheet: the seeded relief-modulated grid (2800px tile
// repeated down the whole scrolling document), shown only inside its
// horizontal zone (single-layer mask) and below the seeded boundary
// (clip-path). Two independent CSS properties multiply — no mask-composite.
// aria-hidden decoration; text is never under here.
function GridLayer({
  image,
  zoneMask,
  clipPath,
}: {
  image: string;
  zoneMask: string;
  clipPath: string;
}) {
  return (
    <div
      aria-hidden
      className={PAPER_LAYER_CLASS}
      style={{
        backgroundImage: `url("${image}")`,
        backgroundSize: "2800px 2800px",
        backgroundRepeat: "repeat",
        mixBlendMode: "multiply",
        WebkitMaskImage: zoneMask,
        maskImage: zoneMask,
        clipPath,
        WebkitClipPath: clipPath,
      }}
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
