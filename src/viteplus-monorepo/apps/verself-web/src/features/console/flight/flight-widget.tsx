import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { elevation, LIVE_ACTIVITY_LIFT } from "./elevation";
import { FlightArc } from "./flight-arc";
import { arcGeometry, tangentDeg } from "./geometry";
import { GitCommitGlyph } from "./git-commit-glyph";
import { IPhoneFrame, type FrameKind } from "./iphone-frame";
import { useFlightMachine } from "./machine";
import type { Flight } from "./model";
import type { PhaseKind } from "./phase";
import { type CornerRadius, Squircle } from "./squircle";

// ── Numeric tokens (single source of truth; the brief tunes these here) ──────

// The whole widget maxes at 598px wide. The card's BLACK BOX is exactly 160px
// tall (padding lives inside it); only the surrounding page gutter is outside.
const WIDGET_MAX_PX = 598;
// CARD_H_PX is a parameter, not a literal sprinkled through the layout. The
// future lock-screen variant (320, not yet designed) is one argument to
// layoutOf — never a forked layout.
const CARD_H_PX = 160;

// ── Safe rectangle (correct-by-construction layout) ─────────────────────────
//
// The card is a 160px-tall bulbous squircle. Content must never enter a corner
// or it clips. Rather than eyeball the padding, we COMPUTE the largest inner
// rectangle that provably clears the corner curve, then lay every band inside
// it.
//
// Key fact that removes all SVG-path parsing: figma's corner-smoothing bows
// the curve OUTWARD (a fuller, less-pointy corner than a plain rounded rect),
// so for any cornerSmoothing ≥ 0 the inward depth of the vendored superellipse
// at a given edge offset is ≤ the inward depth of a *circular* arc of the same
// radius. The circular depth is therefore a strict upper bound: inset content
// by it and it cannot be clipped — independent of the exact smoothing value.
//
// `cornerDepth(x, r)` = how far the boundary intrudes from an edge at offset
// `x` along it: full `r` at the very corner, zero once past the radius (the
// straight-edge region). Quarter-circle of radius r centred (r, r).
function cornerDepth(edgeOffset: number, radius: number): number {
  if (edgeOffset >= radius) return 0;
  const k = radius - edgeOffset;
  return radius - Math.sqrt(radius * radius - k * k);
}

// Aesthetic top/bottom air as a fraction of card height — the reference reads
// markedly more vertical breathing room than the prior fixed 16px (brief note
// c: ≈1.5–2.5×). It is *added on top of* the geometric safety floor, so the
// rendered air is `max(floor, aesthetic)` and clipping is impossible while the
// look is the reference's. Tuned against the deployed render.
const VERTICAL_AIR_RATIO = 0.16;

type Layout = {
  readonly sidePad: number; // horizontal content inset (≈ the prior px-7/px-9)
  readonly padTop: number; // vertical air above content (note c)
  readonly padBottom: number; // ≥ padTop: the bottom corner is the fuller one
  readonly header: number;
  readonly route: number; // remainder — definite px so the arc never collapses
  readonly status: number;
};

// The single layout function. Splits ANY (width,height,radius) into a safe
// inner rectangle and three bands. The 320px lock-screen variant is one
// argument; deterministic px (a %/flex height against an indefinite parent
// collapses the arc to zero).
function layoutOf(cardW: number, cardH: number, radius: CornerRadius): Layout {
  const top = typeof radius === "number" ? radius : radius.top;
  const bottom = typeof radius === "number" ? radius : radius.bottom;
  // Side padding tracks width like the prior responsive px-7 → px-9, now
  // continuous and a single source of truth (it feeds the safety math).
  const sidePad = Math.round(clamp(cardW * 0.062, 24, 38));
  // Provable floor: at horizontal inset `sidePad`, the corner cannot reach
  // deeper than this. Anything ≥ this is in the straight-edge region.
  const air = Math.round(cardH * VERTICAL_AIR_RATIO);
  const padTop = Math.max(Math.ceil(cornerDepth(sidePad, top)), air);
  const padBottom = Math.max(Math.ceil(cornerDepth(sidePad, bottom)), air);
  // Inter-element bands are tight (note c trades inter-element padding for
  // top/bottom air); the route takes the remainder so the arc keeps the
  // dominant central span and may overshoot into the safe air like the
  // reference (the squircle clip, not the band, is the only real bound).
  const header = 18;
  const status = 34; // ≥ pill height; holds the tight status group
  return {
    sidePad,
    padTop,
    padBottom,
    header,
    status,
    route: cardH - padTop - padBottom - header - status,
  };
}

// Asymmetric continuous corners. The prior `{ 28, 46 }` read as a tight top
// over an oblong bottom (brief note i). Both corners grow and converge into a
// bulbous, only-slightly-asymmetric card: the top still tucks marginally
// tighter than the free bottom edge (the iOS Dynamic-Island "wheel"
// metaphor), but the gap is small. cornerSmoothing stays Apple-canonical 0.6
// (the figma plugin default) — the fix is the radius, not the smoothing.
const CARD_RADIUS: CornerRadius = { top: 42, bottom: 52 };
const PILL_RADIUS_RATIO = 0.3;
// iOS fidelity: real San Francisco on Apple devices (the reference context),
// a near-equivalent neo-grotesque elsewhere. Brand Geist is deliberately not
// used here — the whole card stays one type system.
const IOS_FONT =
  '-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", system-ui, sans-serif';

// The region actor (`ASH`) AND both status lines share this weight by
// construction (brief note e: the status group is the same size and weight as
// ASH). One constant so they can never drift apart into a headline/caption
// hierarchy — the only difference between the three is the text itself.
const REGION_WEIGHT = 530;

const INK = "oklch(1 0 0)";
const INK_DIM = "oklch(0.62 0 0)";
const CARD = "oklch(0.05 0 0)";
const NODE_INK = "oklch(0 0 0)";
// The single accent. Sampled from the Flighty reference (#8FF28F) — a soft,
// low-saturation spring-green, deliberately NOT iOS systemGreen (the prior
// pass's value, reverted on the brief's "match the photo / single shade
// everywhere", note f). One value for the arc, both discs and the status
// group; `late` is label-only and never recolors, so there is no second
// accent and no crossfade — the green cannot vary by construction.
const ACCENT = "oklch(0.8767 0.1631 144.03)";
const BUTTON = "oklch(0.7923 0.1443 82.08)";
// The card floats at the Live-Activity elevation level — the framework
// derives the three contact/key/ambient layers from that single choice, so
// the card has no hand-written shadow numbers (brief note 1).
const CARD_SHADOW = elevation(LIVE_ACTIVITY_LIFT);

// ── Scale model (pure: card width → every derived size) ─────────────────────
//
// Composition over ad-hoc numbers. One pure function maps the measured card
// width to the full type/figure scale, so the disc, arrow, arc stroke, marker
// and status group are all *derived* — never magic constants. Brief note (a):
// the disc stays ≈ half the airport-code cap height, but the stroke / dots /
// marker / arrow were 2–3× too heavy and drop hard here.

type Scale = {
  readonly terminalPx: number; // airport-code font size
  readonly discPx: number; // green endpoint disc diameter
  readonly arrowPx: number; // arrow glyph box inside the disc
  readonly arrowStroke: number; // arrow stroke (fine — note a)
  readonly arcStroke: number; // flight-path stroke width (thin — note a)
  readonly markerR: number; // triangle marker circumradius (small — note a)
  readonly headerPx: number; // actor/status size
  readonly statusPx: number; // status group — matches the actor size (note e)
  readonly pillH: number; // commit pill height
};

// SF Pro Display cap-height ÷ font-size. The airport-code "height" the brief
// means is the cap height of the rendered glyphs, not the em box.
const SF_CAP_RATIO = 0.714;
// Brief note (a): "about half the height of the airport code".
const DISC_TO_CAP = 0.52;

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(Math.max(n, lo), hi);
}

function scaleOf(cardW: number, routeH: number): Scale {
  const w = clamp(cardW, 0, WIDGET_MAX_PX);
  // Terminals dominate like SFO/JFK, but the reference's are bold-not-huge
  // (brief note h) AND must fit the route band the layout actually computed —
  // capping by routeH closes the composition loop so the terminals can never
  // overflow the band and crush the arc, at any card height. Width still
  // governs below the cap so narrow cards shrink fluidly.
  const terminalBasisPx = clamp(Math.min(w * 0.085, routeH * 0.7), 28, 46);
  const terminalPx = terminalBasisPx * 0.75;
  const capPx = terminalPx * SF_CAP_RATIO;
  const discPx = Math.round(capPx * DISC_TO_CAP);
  const arrowPx = Math.round(discPx * 0.62);
  // Fine weights — the reference arc is a thin confident line, not a rope.
  const arrowStroke = Math.max(1.25, discPx * 0.085);
  const arcStroke = Math.max(2, Math.round(discPx * 0.17));
  const markerR = Math.max(4, Math.round(discPx * 0.52));
  const headerPx = clamp(terminalBasisPx * 0.33, 12, 16);
  return {
    terminalPx,
    discPx,
    arrowPx,
    arrowStroke,
    arcStroke,
    markerR,
    headerPx,
    statusPx: headerPx, // note e: the status group matches the ASH size
    pillH: clamp(Math.round(terminalBasisPx * 0.74), 28, 40),
  };
}

const useIsomorphicLayoutEffect = typeof window === "undefined" ? () => {} : useLayoutEffect;

// Measures one element's integer box with a ResizeObserver. SSR / pre-measure
// returns null and the caller falls back to a deterministic value, identical
// on the server and the first client render (no hydration shift); the observer
// refines it after mount. Rounded to integers so the squircle clip-path and
// the SVG viewBox never sit on sub-pixel boundaries (brief note k).
function useBox(ref: React.RefObject<HTMLElement | null>): { w: number; h: number } | null {
  const [box, setBox] = useState<{ w: number; h: number } | null>(null);
  useIsomorphicLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const r = el.getBoundingClientRect();
      const w = Math.round(r.width);
      const h = Math.round(r.height);
      setBox((prev) => (prev && prev.w === w && prev.h === h ? prev : { w, h }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return box;
}

// ── Composition root ────────────────────────────────────────────────────────

export function FlightBoard({
  flights,
  orgSlug,
  frame,
  placement = "center",
}: {
  readonly flights: readonly Flight[];
  readonly orgSlug: string;
  readonly frame?: FrameKind | undefined;
  readonly placement?: FlightBoardPlacement;
}) {
  return (
    <FlightCanvas frame={frame} placement={placement}>
      {flights.length === 0 ? (
        <FlightShell>
          <div className="flex flex-1 items-center justify-center">
            <p className="text-sm font-medium" style={{ color: INK_DIM }}>
              No CI in flight
            </p>
          </div>
        </FlightShell>
      ) : (
        flights.map((flight) => <FlightCard key={flight.key} flight={flight} orgSlug={orgSlug} />)
      )}
    </FlightCanvas>
  );
}

export function FlightConsoleSkeleton({
  placement = "center",
}: {
  readonly placement?: FlightBoardPlacement;
}) {
  return (
    <FlightCanvas placement={placement}>
      <FlightShell>
        <div className="flex-1 animate-pulse" style={{ background: "oklch(0.18 0 0)" }} />
      </FlightShell>
    </FlightCanvas>
  );
}

// ── Layout shells ───────────────────────────────────────────────────────────

type FlightBoardPlacement = "center" | "inline";

function FlightCanvas({
  children,
  frame,
  placement,
}: {
  readonly children: ReactNode;
  readonly frame?: FrameKind | undefined;
  readonly placement: FlightBoardPlacement;
}) {
  const board = (
    <div className="flex w-full flex-col gap-5" style={{ maxWidth: WIDGET_MAX_PX }}>
      {children}
    </div>
  );
  // `?...&frame=iphone17` is a dev-only calibration overlay (DESIGN.md) — it
  // renders the card in a pixel-accurate iPhone bezel so the squircle can be
  // tuned 1:1. It is never part of the shipped console surface.
  return (
    <div
      className={
        placement === "inline"
          ? "flex w-full items-start justify-center"
          : "flex min-h-[80svh] w-full items-center justify-center px-5 py-12"
      }
      style={{ fontFamily: IOS_FONT }}
    >
      {frame ? <IPhoneFrame kind={frame}>{board}</IPhoneFrame> : board}
    </div>
  );
}

// One OLED squircle card, exactly CARD_H_PX tall (the black box; padding is
// inside). The padding is the computed safe-rectangle inset, not a literal:
// content placed inside it provably clears the bulbous corners. Empty/skeleton
// states have no measured width, so they fall back to the max-width layout —
// identical card chrome. Depth is the layered shadow, not a tray/border. Font
// smoothing + own compositing layer kill the sub-pixel clip-path shimmer (k).
const DEFAULT_LAYOUT = layoutOf(WIDGET_MAX_PX, CARD_H_PX, CARD_RADIUS);

function FlightShell({
  children,
  layout = DEFAULT_LAYOUT,
}: {
  readonly children: ReactNode;
  readonly layout?: Layout;
}) {
  return (
    <Squircle
      role="article"
      cornerRadius={CARD_RADIUS}
      className="flex flex-col"
      style={{
        height: CARD_H_PX,
        paddingInline: layout.sidePad,
        paddingTop: layout.padTop,
        paddingBottom: layout.padBottom,
        background: CARD,
        boxShadow: CARD_SHADOW,
        transform: "translateZ(0)",
        WebkitFontSmoothing: "antialiased",
        MozOsxFontSmoothing: "grayscale",
        textRendering: "optimizeLegibility",
      }}
    >
      {children}
    </Squircle>
  );
}

// ── Per-run card: clock + state machine ─────────────────────────────────────

function FlightCard({ flight, orgSlug }: { readonly flight: Flight; readonly orgSlug: string }) {
  // The machine owns the clock, conflated sampling, the FSM, monotone progress
  // and springs. The card is a pure Projection consumer — no phase switch.
  const proj = useFlightMachine(flight);

  // Single measurement → one safe-rect layout + one Scale → every derived size
  // below. The layout computes the clip-safe inner rectangle; scale is capped
  // to the route band it produced so nothing downstream can overflow.
  const measureRef = useRef<HTMLDivElement | null>(null);
  const cardBox = useBox(measureRef);
  const measuredW = cardBox?.w ?? WIDGET_MAX_PX;
  const layout = layoutOf(measuredW, CARD_H_PX, CARD_RADIUS);
  const scale = scaleOf(measuredW, layout.route);

  return (
    <div ref={measureRef} className="w-full">
      <FlightShell layout={layout}>
        {/* Three explicit bands summing to the safe rectangle — header / route /
            status. The route band has a definite height so the arc fills it. */}
        <div className="flex flex-col">
          <div
            className="flex shrink-0 items-center justify-between"
            style={{ height: layout.header }}
          >
            <FlightHeader actor={flight.actorLabel} scale={scale} />
          </div>
          <div className="shrink-0" style={{ height: layout.route }}>
            <FlightRoute
              source={flight.sourceLabel}
              dest={flight.destLabel}
              progress={proj.progressTarget}
              phaseKind={proj.phaseKind}
              scale={scale}
            />
          </div>
          <div
            className="flex shrink-0 items-end justify-between gap-4"
            style={{ height: layout.status }}
          >
            <FlightStatus
              headline={proj.headline}
              detail={proj.detail}
              accent={ACCENT}
              scale={scale}
            />
            {flight.commitPill !== null ? (
              <CommitPill
                orgSlug={orgSlug}
                providerRunId={flight.providerRunId}
                count={flight.commitPill}
              />
            ) : null}
          </div>
        </div>
      </FlightShell>
    </div>
  );
}

// ── Leaves ──────────────────────────────────────────────────────────────────

// Header row: the region actor keeps near-zero tracking so a short all-caps
// run like ASH reads as one tight word, not spaced-out letters (brief note g).
function FlightHeader({ actor, scale }: { readonly actor: string; readonly scale: Scale }) {
  return (
    <div>
      <p
        className="tracking-[0.01em]"
        style={{ color: INK, fontSize: scale.headerPx, fontWeight: REGION_WEIGHT }}
      >
        {actor}
      </p>
    </div>
  );
}

const ROUTE_FALLBACK = { w: 260, h: 60 } as const; // SSR / pre-measure

function FlightRoute({
  source,
  dest,
  progress,
  phaseKind,
  scale,
}: {
  readonly source: string;
  readonly dest: string;
  readonly progress: number;
  readonly phaseKind: PhaseKind;
  readonly scale: Scale;
}) {
  // The route band owns the measured box and builds the bezier ONCE; the arc
  // consumes it and so do both endpoint arrows — single geometry source
  // (brief note b). Endpoints inset by the disc radius, so the curve provably
  // runs disc-centre → disc-centre.
  const pgRef = useRef<HTMLDivElement | null>(null);
  const pg = useBox(pgRef);
  const w = pg && pg.w > 0 ? pg.w : ROUTE_FALLBACK.w;
  const h = pg && pg.h > 0 ? pg.h : ROUTE_FALLBACK.h;
  const bezier = arcGeometry({ width: w, height: h, inset: scale.discPx / 2 });
  const outboundDeg = tangentDeg(bezier, 0); // climb-out, into the source disc
  const inboundDeg = tangentDeg(bezier, 1); // descent, into the dest disc

  return (
    <div className="flex h-full items-center">
      <div className="flex h-full w-full items-center gap-2 sm:gap-3">
        <Terminal label={source} scale={scale} />
        {/* Path group: the arc fills the whole route band absolutely; the
            discs sit on top at the exact bezier endpoints, so the curve reads
            as one continuous line tucked under both discs. */}
        <div ref={pgRef} className="relative flex h-full flex-[1_1_54%] items-center">
          <FlightArc
            bezier={bezier}
            width={w}
            height={h}
            progress={progress}
            accent={ACCENT}
            phaseKind={phaseKind}
            strokeWidth={scale.arcStroke}
            markerR={scale.markerR}
          />
          <Endpoint
            accent={ACCENT}
            scale={scale}
            className="absolute left-0 top-1/2 -translate-y-1/2"
          >
            <ArrowGlyph deg={outboundDeg} px={scale.arrowPx} stroke={scale.arrowStroke} />
          </Endpoint>
          <Endpoint
            accent={ACCENT}
            scale={scale}
            className="absolute right-0 top-1/2 -translate-y-1/2"
          >
            <ArrowGlyph deg={inboundDeg} px={scale.arrowPx} stroke={scale.arrowStroke} />
          </Endpoint>
        </div>
        <Terminal label={dest} scale={scale} />
      </div>
    </div>
  );
}

// Terminals are the visual mass (the SFO/JFK proportion) but bold-not-heavy
// (brief note h): weight 620, capped size, no-wrap + shrink-0 so the arc keeps
// the dominant central span at 598px and narrower.
function Terminal({ label, scale }: { readonly label: string; readonly scale: Scale }) {
  return (
    <span
      className="shrink-0 whitespace-nowrap tracking-normal"
      style={{ color: INK, fontSize: scale.terminalPx, fontWeight: 620, lineHeight: 1 }}
    >
      {label}
    </span>
  );
}

function Endpoint({
  accent,
  scale,
  className,
  children,
}: {
  readonly accent: string;
  readonly scale: Scale;
  readonly className?: string;
  readonly children: ReactNode;
}) {
  return (
    <span
      className={`z-10 flex shrink-0 items-center justify-center rounded-full ${className ?? ""}`}
      style={{ width: scale.discPx, height: scale.discPx, background: accent, color: NODE_INK }}
    >
      {children}
    </span>
  );
}

// One right-pointing arrow rotated to the bezier tangent (brief note b) — the
// disc arrows carry the curve's climb-out / descent direction rather than a
// hardcoded 45° glyph. A single glyph; the rotation makes takeoff and landing
// fall out of the geometry, not two different icons.
function ArrowGlyph({
  deg,
  px,
  stroke,
}: {
  readonly deg: number;
  readonly px: number;
  readonly stroke: number;
}) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={px}
      height={px}
      fill="none"
      stroke="currentColor"
      strokeWidth={stroke * (24 / px)}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      style={{ transform: `rotate(${deg.toFixed(2)}deg)`, transformOrigin: "50% 50%" }}
    >
      <path d="M4 12h15M13 6l6 6-6 6" />
    </svg>
  );
}

// Bottom-left status as ONE tight group (brief note e). Both lines are the
// SAME single green, the SAME size, and the SAME weight as the region actor
// (`REGION_WEIGHT`) — no luminance dim, no caption weight. The brief is
// explicit that the pair matches `ASH`, and "single shade everywhere" (note f)
// forbids a dimmed second hue, so the only thing distinguishing the two lines
// is their text. Leadings collapsed so it reads as one tight unit.
function FlightStatus({
  headline,
  detail,
  accent,
  scale,
}: {
  readonly headline: string;
  readonly detail: string;
  readonly accent: string;
  readonly scale: Scale;
}) {
  const lineStyle = {
    color: accent,
    fontSize: scale.statusPx,
    fontWeight: REGION_WEIGHT,
    lineHeight: 1.18,
  } as const;
  return (
    <div className="min-w-0">
      <p style={lineStyle}>{headline}</p>
      <p style={lineStyle}>{detail}</p>
    </div>
  );
}

// The bottom-right interactive piece uses the compact repo-native glyph and a
// tabular count so the live activity stays legible at small sizes.
function CommitPill({
  orgSlug,
  providerRunId,
  count,
}: {
  readonly orgSlug: string;
  readonly providerRunId: string;
  readonly count: string;
}) {
  return (
    <Link
      className="inline-flex shrink-0 items-center font-bold leading-none transition-opacity hover:opacity-90"
      style={{
        height: 28,
        paddingInline: 10,
        gap: 6,
        borderRadius: Math.round(28 * PILL_RADIUS_RATIO),
        background: BUTTON,
        color: NODE_INK,
        fontSize: 13,
      }}
      to="/$orgSlug/runs/$providerRunId"
      params={{ orgSlug, providerRunId }}
      title="CI run logs"
    >
      <GitCommitGlyph style={{ height: 12, width: "auto", color: NODE_INK }} aria-hidden="true" />
      <span className="tabular-nums">{count}</span>
    </Link>
  );
}
