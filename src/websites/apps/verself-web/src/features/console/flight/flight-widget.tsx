import { type ReactNode, useLayoutEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { WINGS_CROPPED_VIEWBOX, WINGS_PATH_D } from "@verself/brand/components/wings";
import { FlightArc } from "./flight-arc";
import { GitCommitGlyph } from "./git-commit-glyph";
import { useAccentSpring, useFlightMachine } from "./machine";
import type { Flight } from "./model";
import type { PhaseKind } from "./phase";
import { type CornerRadius, Squircle, useSquircle } from "./squircle";

// ── Numeric tokens (single source of truth; the brief tunes these here) ──────

// The whole widget maxes at 598px wide. The card's BLACK BOX is exactly 160px
// tall (padding lives inside it); only the surrounding page gutter is outside.
const WIDGET_MAX_PX = 598;
const CARD_H_PX = 160;

// Asymmetric continuous corners: an iOS Live Activity is a fixed 3-D corner
// "wheel" projected onto the lock-screen — the top edge tucks under the
// Dynamic Island (tighter radius) while the free bottom edge rides a fuller
// part of the wheel. figma-squircle's per-corner distribution keeps all four
// on Apple's superellipse while top ≠ bottom, so vertical swipes stay seamless.
const CARD_RADIUS: CornerRadius = { top: 28, bottom: 46 };
// Pill radius is held well under its half-height so the corner reads as a
// squircle rounded-rectangle, not a stadium.
const PILL_RADIUS = 12;
const CHIP_RADIUS = 6;

// iOS fidelity: the real San Francisco on Apple devices (the reference
// context), a near-equivalent neo-grotesque elsewhere. Brand Geist is
// deliberately not used here.
const IOS_FONT =
  '-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", system-ui, sans-serif';

const INK = "oklch(1 0 0)";
const INK_DIM = "oklch(0.62 0 0)";
const CARD = "oklch(0.05 0 0)";
const NODE_INK = "oklch(0 0 0)";

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
// width to the full type/figure scale, so the disc, arrow, arc stroke and
// marker are all *derived* from the airport-code size — never magic constants.
// Brief note (a): the disc diameter stays ≈ half the airport-code height.

type Scale = {
  readonly terminalPx: number; // airport-code font size
  readonly discPx: number; // green endpoint disc diameter
  readonly arrowPx: number; // arrow glyph inside the disc
  readonly arcStroke: number; // flight-path stroke width
  readonly markerR: number; // triangle marker circumradius
  readonly headerPx: number; // actor / wordmark size
};

// SF Pro Display: cap-height ÷ font-size. The airport-code "height" the brief
// means is the cap height of the rendered glyphs, not the em box.
const SF_CAP_RATIO = 0.714;
// Brief note (a): "about half the height of the airport code". Tunable.
const DISC_TO_CAP = 0.52;

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(Math.max(n, lo), hi);
}

function scaleOf(cardW: number): Scale {
  const w = clamp(cardW, 0, WIDGET_MAX_PX);
  // Terminals dominate like SFO/JFK in the reference; calibrated so a
  // full-width card lands ~52px and never starves below ~30px.
  const terminalPx = clamp(w * 0.099, 30, 52);
  const capPx = terminalPx * SF_CAP_RATIO;
  const discPx = Math.round(capPx * DISC_TO_CAP);
  const arrowPx = Math.round(discPx * 0.62);
  const arcStroke = Math.max(4, Math.round(discPx * 0.42));
  const markerR = Math.max(6, Math.round(discPx * 0.7));
  const headerPx = clamp(terminalPx * 0.3, 12, 16);
  return { terminalPx, discPx, arrowPx, arcStroke, markerR, headerPx };
}

const useIsomorphicLayoutEffect = typeof window === "undefined" ? () => {} : useLayoutEffect;

// Measures one element's width with a ResizeObserver. SSR / pre-measure
// returns null and the caller falls back to the max-width scale, which is
// identical on the server and the first client render (no hydration shift);
// the observer refines it after mount. Same sanctioned DOM-measurement
// exception as <Squircle> / <FlightArc>.
function useMeasuredWidth(ref: React.RefObject<HTMLElement | null>): number | null {
  const [width, setWidth] = useState<number | null>(null);
  useIsomorphicLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      const w = el.getBoundingClientRect().width;
      setWidth((prev) => (prev !== null && Math.abs(prev - w) < 0.5 ? prev : w));
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [ref]);
  return width;
}

// ── Composition root ────────────────────────────────────────────────────────

export function FlightBoard({
  flights,
  orgSlug,
}: {
  readonly flights: readonly Flight[];
  readonly orgSlug: string;
}) {
  return (
    <FlightCanvas>
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

function FlightCanvas({ children }: { readonly children: ReactNode }) {
  return (
    <div
      className="flex min-h-[80svh] w-full items-center justify-center px-5 py-12"
      style={{ fontFamily: IOS_FONT }}
    >
      <div className="flex w-full flex-col gap-5" style={{ maxWidth: WIDGET_MAX_PX }}>
        {children}
      </div>
    </div>
  );
}

// One OLED squircle card, exactly 160px tall (the black box; padding is
// inside). Depth is the layered shadow, not a tray/border, so it reads as an
// elevated iOS Live Activity floating on the page.
function FlightShell({ children }: { readonly children: ReactNode }) {
  return (
    <Squircle
      role="article"
      cornerRadius={CARD_RADIUS}
      className="flex flex-col justify-between px-7 py-4 sm:px-9"
      style={{ height: CARD_H_PX, background: CARD, boxShadow: CARD_SHADOW }}
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
  const cardW = useMeasuredWidth(measureRef);
  const scale = scaleOf(cardW ?? WIDGET_MAX_PX);

  return (
    <FlightShell>
      <div ref={measureRef} className="flex h-full flex-col justify-between">
        <FlightHeader actor={flight.actorLabel} scale={scale} />
        <FlightRoute
          source={flight.sourceLabel}
          dest={flight.destLabel}
          accent={accent}
          progress={proj.progressTarget}
          phaseKind={proj.phaseKind}
          scale={scale}
        />
        <div className="flex items-end justify-between gap-4">
          <FlightStatus headline={proj.headline} detail={proj.detail} accent={accent} />
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
  );
}

// ── Leaves ──────────────────────────────────────────────────────────────────

// Header row: the region actor (left) and the Guardian lockup (right),
// mirroring the reference's "FL234 … ✈ FLIGHTY". The region label keeps a
// touch of tracking for the all-caps run, but far less than before so "ASH"
// reads as a word, not spaced-out letters (brief note g).
function FlightHeader({ actor, scale }: { readonly actor: string; readonly scale: Scale }) {
  return (
    <div className="flex items-center justify-between">
      <p className="font-medium tracking-[0.05em]" style={{ color: INK, fontSize: scale.headerPx }}>
        {actor}
      </p>
      <GuardianMark sizePx={scale.headerPx} />
    </div>
  );
}

// The Verself house mark: the Guardian wings in OLED-black inside a small white
// squircle chip (our own corner primitive, so the chip is a true squircle),
// then the GUARDIAN wordmark — the reference's white-chip + dark-glyph +
// wordmark lockup, rebuilt on-brand.
function GuardianMark({ sizePx }: { readonly sizePx: number }) {
  const chip = Math.round(sizePx * 1.55);
  return (
    <div className="flex items-center gap-2">
      <Squircle
        cornerRadius={CHIP_RADIUS}
        className="flex items-center justify-center"
        style={{ width: chip, height: chip, background: INK }}
        aria-hidden="true"
      >
        <svg
          viewBox={WINGS_CROPPED_VIEWBOX}
          style={{ width: "62%", height: "62%" }}
          role="presentation"
          focusable="false"
        >
          <path d={WINGS_PATH_D} fill={CARD} />
        </svg>
      </Squircle>
      <span className="font-semibold tracking-[0.14em]" style={{ color: INK, fontSize: sizePx }}>
        GUARDIAN
      </span>
    </div>
  );
}

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
  return (
    <div className="flex flex-1 items-center">
      <div className="flex w-full items-center gap-2 sm:gap-3">
        <Terminal label={source} scale={scale} />
        {/* Path group: the arc fills the whole route band absolutely; the
            discs sit on top at the exact bezier endpoints, so the curve
            provably runs disc-centre → disc-centre as one continuous line.
            h-full so the arc has the full vertical band for a generous lob. */}
        <div className="relative flex h-full flex-[1_1_54%] items-center">
          <FlightArc
            progress={progress}
            accent={accent}
            phaseKind={phaseKind}
            discPx={scale.discPx}
            strokeWidth={scale.arcStroke}
            markerR={scale.markerR}
          />
          <Endpoint
            accent={accent}
            scale={scale}
            className="absolute left-0 top-1/2 -translate-y-1/2"
          >
            <ArrowUpRight style={{ width: scale.arrowPx, height: scale.arrowPx }} strokeWidth={3} />
          </Endpoint>
          <Endpoint
            accent={accent}
            scale={scale}
            className="absolute right-0 top-1/2 -translate-y-1/2"
          >
            <ArrowDownRight
              style={{ width: scale.arrowPx, height: scale.arrowPx }}
              strokeWidth={3}
            />
          </Endpoint>
        </div>
        <Terminal label={dest} scale={scale} />
      </div>
    </div>
  );
}

// Terminals are the visual mass (the SFO/JFK proportion): capped size +
// no-wrap + shrink-0 so the arc keeps the dominant central span at 598px and
// narrower.
function Terminal({ label, scale }: { readonly label: string; readonly scale: Scale }) {
  return (
    <span
      className="shrink-0 whitespace-nowrap font-bold tracking-[-0.01em]"
      style={{ color: INK, fontSize: scale.terminalPx, lineHeight: 1 }}
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

// Bottom-left status, two tight rows (brief note e: no vertical padding; the
// detail sits directly under the headline like the reference's "On Time /
// Landing in 55m").
function FlightStatus({
  headline,
  detail,
  accent,
}: {
  readonly headline: string;
  readonly detail: string;
  readonly accent: string;
}) {
  return (
    <div className="min-w-0 leading-tight">
      <p className="text-[1.0625rem] font-semibold leading-tight" style={{ color: accent }}>
        {headline}
      </p>
      <p
        className="mt-0.5 text-sm font-medium leading-tight"
        style={{ color: accent, opacity: 0.82 }}
      >
        {detail}
      </p>
    </div>
  );
}

// The bottom-right interactive piece — its own <Squircle>, a fixed integer-px
// box so the clip-path never hairline-aliases (brief note d), compact like the
// reference's baggage pill. Links to the run's logs.
function CommitPill({
  orgSlug,
  providerRunId,
  count,
}: {
  readonly orgSlug: string;
  readonly providerRunId: string;
  readonly count: string;
}) {
  // The pill is a <Link>, so it takes the squircle via the hook rather than
  // the div wrapper — same single radius source, full Link typing preserved.
  const sq = useSquircle<HTMLAnchorElement>(PILL_RADIUS);
  return (
    <Link
      ref={sq.ref}
      className="inline-flex shrink-0 items-center justify-center gap-1.5 text-[0.9375rem] font-bold leading-none transition-opacity hover:opacity-90"
      style={{
        ...sq.style,
        height: 34,
        paddingInline: 13,
        background: "oklch(0.80 0.16 70)",
        color: NODE_INK,
      }}
      to="/$orgSlug/runs/$providerRunId"
      params={{ orgSlug, providerRunId }}
      title="CI run logs"
    >
      <GitCommitGlyph style={{ width: "1.05em", height: "1.05em" }} />
      <span className="tabular-nums">{count}</span>
    </Link>
  );
}
