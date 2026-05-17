import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { WINGS_CROPPED_VIEWBOX, WINGS_PATH_D } from "@verself/brand/components/wings";
import { FlightArc } from "./flight-arc";
import { arcGeometry, tangentDeg } from "./geometry";
import { GitCommitGlyph } from "./git-commit-glyph";
import { IPhoneFrame, type FrameKind } from "./iphone-frame";
import { useAccentSpring, useFlightMachine } from "./machine";
import type { Flight } from "./model";
import type { PhaseKind } from "./phase";
import { concentric, type CornerRadius, Squircle, useSquircle } from "./squircle";

// ── Numeric tokens (single source of truth; the brief tunes these here) ──────

// The whole widget maxes at 598px wide. The card's BLACK BOX is exactly 160px
// tall (padding lives inside it); only the surrounding page gutter is outside.
const WIDGET_MAX_PX = 598;
// CARD_H_PX is a parameter, not a literal sprinkled through the layout. The
// future lock-screen variant (320, not yet designed) is one argument to
// bandsOf — never a forked layout.
const CARD_H_PX = 160;

// Deterministic vertical layout. bandsOf splits ANY card height into three
// explicit bands so the route gets a definite, generous height for the arc;
// percentage/flex height against an indefinite parent collapses the arc to
// zero, so the band heights are computed, not flexed. Interior padding is
// generous (brief note c asks for more top/bottom air than the prior pass).
type Bands = {
  readonly pad: number;
  readonly header: number;
  readonly route: number;
  readonly status: number;
};
function bandsOf(height: number): Bands {
  const pad = 18; // vertical padding inside the box (was 14 — note c)
  const header = 22;
  const status = 40; // ≥ pill height; holds the tight status group
  return { pad, header, status, route: height - 2 * pad - header - status };
}

// Asymmetric continuous corners. The prior `{ 28, 46 }` read as a tight top
// over an oblong bottom (brief note i). Both corners grow and converge into a
// bulbous, only-slightly-asymmetric card: the top still tucks marginally
// tighter than the free bottom edge (the iOS Dynamic-Island "wheel"
// metaphor), but the gap is small. cornerSmoothing stays Apple-canonical 0.6
// (the figma plugin default) — the fix is the radius, not the smoothing.
const CARD_RADIUS: CornerRadius = { top: 42, bottom: 52 };
// Pill radius held well under its half-height so the corner reads as a
// squircle rounded-rectangle, not a stadium.
const PILL_RADIUS = 13;
// The commit-glyph well is inset inside the pill; its radius nests on Apple's
// nested-rounded-rect rule rather than an ad-hoc value.
const WELL_INSET = 4;

// iOS fidelity: real San Francisco on Apple devices (the reference context),
// a near-equivalent neo-grotesque elsewhere. Brand Geist is deliberately not
// used here — the whole card stays one type system.
const IOS_FONT =
  '-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", system-ui, sans-serif';

const INK = "oklch(1 0 0)";
const INK_DIM = "oklch(0.62 0 0)";
const CARD = "oklch(0.05 0 0)";
const NODE_INK = "oklch(0 0 0)";
const ACCENT_AMBER = "oklch(0.8 0.16 70)";
// iOS systemOrange (dark) — the commit glyph reads as orange light through the
// punched well (brief note d's negative-space treatment).
const IOS_ORANGE = "oklch(0.7824 0.1711 67.22)";

// Live-Activity elevation: one overhead light source, three stacked layers —
// a tight contact shadow that grounds the card, a medium key shadow that is
// the readable drop, and a wide soft ambient shadow (negative spread) that
// lifts it off the page. Opacity falls as blur grows; offsets stay vertical.
const CARD_SHADOW =
  "0 1px 2px oklch(0 0 0 / 0.07), " +
  "0 5px 12px -4px oklch(0 0 0 / 0.16), " +
  "0 22px 46px -16px oklch(0 0 0 / 0.3)";

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
  readonly headerPx: number; // actor / house-mark size
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

function scaleOf(cardW: number): Scale {
  const w = clamp(cardW, 0, WIDGET_MAX_PX);
  // Terminals dominate like SFO/JFK, but the reference's are bold-not-huge:
  // a touch under the prior pass (brief note h).
  const terminalPx = clamp(w * 0.085, 28, 46);
  const capPx = terminalPx * SF_CAP_RATIO;
  const discPx = Math.round(capPx * DISC_TO_CAP);
  const arrowPx = Math.round(discPx * 0.62);
  // Fine weights — the reference arc is a thin confident line, not a rope.
  const arrowStroke = Math.max(1.25, discPx * 0.085);
  const arcStroke = Math.max(2, Math.round(discPx * 0.17));
  const markerR = Math.max(4, Math.round(discPx * 0.52));
  const headerPx = clamp(terminalPx * 0.33, 12, 16);
  return {
    terminalPx,
    discPx,
    arrowPx,
    arrowStroke,
    arcStroke,
    markerR,
    headerPx,
    statusPx: headerPx, // note e: the status group matches the ASH size
    pillH: clamp(Math.round(terminalPx * 0.74), 28, 40),
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
}: {
  readonly flights: readonly Flight[];
  readonly orgSlug: string;
  readonly frame?: FrameKind | undefined;
}) {
  return (
    <FlightCanvas frame={frame}>
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

export function FlightConsoleSkeleton() {
  return (
    <FlightCanvas>
      <FlightShell>
        <div className="flex-1 animate-pulse" style={{ background: "oklch(0.18 0 0)" }} />
      </FlightShell>
    </FlightCanvas>
  );
}

// ── Layout shells ───────────────────────────────────────────────────────────

function FlightCanvas({
  children,
  frame,
}: {
  readonly children: ReactNode;
  readonly frame?: FrameKind | undefined;
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
      className="flex min-h-[80svh] w-full items-center justify-center px-5 py-12"
      style={{ fontFamily: IOS_FONT }}
    >
      {frame ? <IPhoneFrame kind={frame}>{board}</IPhoneFrame> : board}
    </div>
  );
}

// One OLED squircle card, exactly CARD_H_PX tall (the black box; padding is
// inside). Depth is the layered shadow, not a tray/border. Font smoothing +
// own compositing layer kill the sub-pixel clip-path/text shimmer (note k).
function FlightShell({ children }: { readonly children: ReactNode }) {
  return (
    <Squircle
      role="article"
      cornerRadius={CARD_RADIUS}
      className="flex flex-col px-7 sm:px-9"
      style={{
        height: CARD_H_PX,
        paddingBlock: bandsOf(CARD_H_PX).pad,
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
  const accent = useAccentSpring(proj.accent);

  // Single measurement → single Scale → every derived size below.
  const measureRef = useRef<HTMLDivElement | null>(null);
  const cardBox = useBox(measureRef);
  const scale = scaleOf(cardBox?.w ?? WIDGET_MAX_PX);
  const bands = bandsOf(CARD_H_PX);

  return (
    <FlightShell>
      {/* Three explicit bands summing to the content box — header / route /
          status. The route band has a definite height so the arc fills it. */}
      <div ref={measureRef} className="flex flex-col">
        <div
          className="flex shrink-0 items-center justify-between"
          style={{ height: bands.header }}
        >
          <FlightHeader actor={flight.actorLabel} scale={scale} />
        </div>
        <div className="shrink-0" style={{ height: bands.route }}>
          <FlightRoute
            source={flight.sourceLabel}
            dest={flight.destLabel}
            accent={accent}
            progress={proj.progressTarget}
            phaseKind={proj.phaseKind}
            scale={scale}
          />
        </div>
        <div
          className="flex shrink-0 items-end justify-between gap-4"
          style={{ height: bands.status }}
        >
          <FlightStatus
            headline={proj.headline}
            detail={proj.detail}
            accent={accent}
            scale={scale}
          />
          {flight.commitPill !== null ? (
            <CommitPill
              orgSlug={orgSlug}
              providerRunId={flight.providerRunId}
              count={flight.commitPill}
              scale={scale}
            />
          ) : null}
        </div>
      </div>
    </FlightShell>
  );
}

// ── Leaves ──────────────────────────────────────────────────────────────────

// Header row: the region actor (left) and the house mark (right), mirroring
// the reference's "FL234 … ✈ FLIGHTY". The actor keeps near-zero tracking so a
// short all-caps run like ASH reads as one tight word, not spaced-out letters
// (brief note g).
function FlightHeader({ actor, scale }: { readonly actor: string; readonly scale: Scale }) {
  return (
    <>
      <p
        className="tracking-[0.01em]"
        style={{ color: INK, fontSize: scale.headerPx, fontWeight: 530 }}
      >
        {actor}
      </p>
      <HouseMark sizePx={scale.headerPx} />
    </>
  );
}

// Bare white wings — no chip, no GUARDIAN wordmark (brief note j → bare). The
// same argent glyph the company masthead carries, painted INK, sized quiet.
// The cropped viewBox is glyph-hugging (portrait), so width tracks its aspect.
function HouseMark({ sizePx }: { readonly sizePx: number }) {
  const h = Math.round(sizePx * 1.18);
  const w = Math.round(h * (102.174 / 120.823));
  return (
    <svg
      viewBox={WINGS_CROPPED_VIEWBOX}
      style={{ width: w, height: h }}
      role="presentation"
      focusable="false"
      aria-hidden="true"
    >
      <path d={WINGS_PATH_D} fill={INK} />
    </svg>
  );
}

const ROUTE_FALLBACK = { w: 260, h: 60 } as const; // SSR / pre-measure

function FlightRoute({
  source,
  dest,
  accent,
  progress,
  phaseKind,
  scale,
}: {
  readonly source: string;
  readonly dest: string;
  readonly accent: string;
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
            accent={accent}
            phaseKind={phaseKind}
            strokeWidth={scale.arcStroke}
            markerR={scale.markerR}
          />
          <Endpoint
            accent={accent}
            scale={scale}
            className="absolute left-0 top-1/2 -translate-y-1/2"
          >
            <ArrowGlyph deg={outboundDeg} px={scale.arrowPx} stroke={scale.arrowStroke} />
          </Endpoint>
          <Endpoint
            accent={accent}
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
      className="shrink-0 whitespace-nowrap tracking-[-0.01em]"
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

// Bottom-left status as ONE tight group (brief note e): both lines at the
// actor/header size, leadings collapsed, near-identical weight so the
// difference is luminance not boldness — the headline carries full accent,
// the detail the same hue dimmed; it reads as a unit, not headline + caption.
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
  return (
    <div className="min-w-0">
      <p style={{ color: accent, fontSize: scale.statusPx, fontWeight: 590, lineHeight: 1.18 }}>
        {headline}
      </p>
      <p
        style={{
          color: accent,
          fontSize: scale.statusPx,
          fontWeight: 560,
          lineHeight: 1.18,
          opacity: 0.62,
        }}
      >
        {detail}
      </p>
    </div>
  );
}

// The bottom-right interactive piece — its own <Squircle> with an integer-px
// box (so the clip-path never hairline-aliases, note k). The amber pill holds
// a dark WELL (a concentric <Squircle>) and the commit glyph reads as iOS
// orange light through it — negative space, not ink on amber (brief note d).
// The count stays black on the amber, like the reference. Links to the logs.
function CommitPill({
  orgSlug,
  providerRunId,
  count,
  scale,
}: {
  readonly orgSlug: string;
  readonly providerRunId: string;
  readonly count: string;
  readonly scale: Scale;
}) {
  // The pill is a <Link>, so it takes the squircle via the hook rather than
  // the div wrapper — same single radius source, full Link typing preserved.
  const sq = useSquircle<HTMLAnchorElement>(PILL_RADIUS);
  const wellSize = scale.pillH - 2 * WELL_INSET;
  const glyphPx = Math.round(wellSize * 0.62);
  return (
    <Link
      ref={sq.ref}
      className="inline-flex shrink-0 items-center font-bold leading-none transition-opacity hover:opacity-90"
      style={{
        ...sq.style,
        height: scale.pillH,
        paddingInline: WELL_INSET,
        gap: Math.round(scale.pillH * 0.22),
        background: ACCENT_AMBER,
        color: NODE_INK,
        fontSize: Math.round(scale.statusPx * 0.95),
      }}
      to="/$orgSlug/runs/$providerRunId"
      params={{ orgSlug, providerRunId }}
      title="CI run logs"
    >
      <Squircle
        cornerRadius={concentric(PILL_RADIUS, WELL_INSET)}
        className="flex items-center justify-center"
        style={{ width: wellSize, height: wellSize, background: CARD }}
        aria-hidden="true"
      >
        <GitCommitGlyph style={{ width: glyphPx, height: glyphPx, color: IOS_ORANGE }} />
      </Squircle>
      <span className="tabular-nums" style={{ paddingRight: Math.round(scale.pillH * 0.28) }}>
        {count}
      </span>
    </Link>
  );
}
