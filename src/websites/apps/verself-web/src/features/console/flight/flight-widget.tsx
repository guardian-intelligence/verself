import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { FlightArc } from "./flight-arc";
import { GitCommitGlyph } from "./git-commit-glyph";
import { useAccentSpring, useFlightMachine } from "./machine";
import type { Flight } from "./model";
import type { PhaseKind } from "./phase";
import { Squircle, useSquircle } from "./squircle";

// The whole widget maxes at 598px. There is no light tray — depth comes from
// a layered shadow, not a border (see CARD_SHADOW).
const WIDGET_MAX_PX = 598;
const CARD_RADIUS_PX = 40; // single squircle card
// Squircle rounded-rect, deliberately well under the pill's half-height so the
// vendored iOS corner math renders a rounded rectangle, not a stadium.
const PILL_RADIUS_PX = 13;

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

// One OLED squircle card. Depth is the layered shadow, not a tray/border, so
// it reads as an elevated iOS Live Activity floating on the page.
function FlightShell({ children }: { readonly children: ReactNode }) {
  return (
    <Squircle
      role="article"
      cornerRadius={CARD_RADIUS_PX}
      className="flex min-h-[15.5rem] flex-col justify-between px-9 py-8 sm:px-11 sm:py-9"
      style={{ background: CARD, boxShadow: CARD_SHADOW }}
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

  return (
    <FlightShell>
      <FlightHeader actor={flight.actorLabel} />
      <FlightRoute
        source={flight.sourceLabel}
        dest={flight.destLabel}
        accent={accent}
        progress={proj.progressTarget}
        phaseKind={proj.phaseKind}
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
    </FlightShell>
  );
}

// ── Leaves ──────────────────────────────────────────────────────────────────

function FlightHeader({ actor }: { readonly actor: string }) {
  return (
    <p className="text-sm font-medium tracking-[0.14em]" style={{ color: INK }}>
      {actor}
    </p>
  );
}

function FlightRoute({
  source,
  dest,
  accent,
  progress,
  phaseKind,
}: {
  readonly source: string;
  readonly dest: string;
  readonly accent: string;
  readonly progress: number;
  readonly phaseKind: PhaseKind;
}) {
  return (
    <div className="flex flex-1 items-center py-4">
      <div className="flex w-full items-center gap-3 sm:gap-4">
        <Terminal label={source} />
        {/* The path group: endpoints + arc with no gap, so the arc tucks
            UNDER the circles (negative margin + z-order) and reads as one
            continuous line emerging from each disc. */}
        <div className="flex flex-[1_1_40%] items-center">
          <Endpoint accent={accent}>
            <ArrowUpRight className="size-[1.05rem]" strokeWidth={2.75} />
          </Endpoint>
          <FlightArc progress={progress} accent={accent} phaseKind={phaseKind} />
          <Endpoint accent={accent}>
            <ArrowDownRight className="size-[1.05rem]" strokeWidth={2.75} />
          </Endpoint>
        </div>
        <Terminal label={dest} />
      </div>
    </div>
  );
}

// Terminals are bold but must not starve the arc: capped size + no-wrap +
// shrink-0 so the arc keeps the dominant central span (the Flighty
// proportion), matching the reference at the 598px width and narrower.
function Terminal({ label }: { readonly label: string }) {
  return (
    <span
      className="shrink-0 whitespace-nowrap text-[clamp(1.75rem,7.5vw,3rem)] font-bold tracking-[-0.01em]"
      style={{ color: INK }}
    >
      {label}
    </span>
  );
}

function Endpoint({ accent, children }: { readonly accent: string; readonly children: ReactNode }) {
  return (
    <span
      className="relative z-10 flex size-9 shrink-0 items-center justify-center rounded-full"
      style={{ background: accent, color: NODE_INK }}
    >
      {children}
    </span>
  );
}

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
    <div className="min-w-0">
      <p className="text-[1.0625rem] font-semibold leading-tight" style={{ color: accent }}>
        {headline}
      </p>
      <p className="mt-1 text-sm font-medium" style={{ color: accent, opacity: 0.82 }}>
        {detail}
      </p>
    </div>
  );
}

// The bottom-right interactive piece — its own <Squircle> per brief.
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
  const sq = useSquircle<HTMLAnchorElement>(PILL_RADIUS_PX);
  return (
    <Link
      ref={sq.ref}
      className="inline-flex shrink-0 items-center gap-1.5 px-3.5 py-2 text-base font-bold transition-opacity hover:opacity-90"
      style={{ ...sq.style, background: "oklch(0.80 0.16 70)", color: NODE_INK }}
      to="/$orgSlug/runs/$providerRunId"
      params={{ orgSlug, providerRunId }}
      title="CI run logs"
    >
      <GitCommitGlyph className="size-[1.2rem]" />
      <span className="tabular-nums">{count}</span>
    </Link>
  );
}
