import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { FlightArc } from "./flight-arc";
import { GitCommitGlyph } from "./git-commit-glyph";
import { useAccentSpring, useFlightMachine } from "./machine";
import type { Flight } from "./model";
import type { PhaseKind } from "./phase";
import { concentric, Squircle, useSquircle } from "./squircle";

// Single source of truth: the WHOLE widget — light tray included — maxes at
// exactly 598px (the maxWidth wrapper constrains the tray's border box). The
// OLED card is therefore 598 − 2×TRAY_INSET_PX of content + its own padding.
const WIDGET_MAX_PX = 598;
const TRAY_INSET_PX = 7;
const TRAY_RADIUS_PX = 46; // TODO(audit): tuned to the reference
const CARD_RADIUS_PX = concentric(TRAY_RADIUS_PX, TRAY_INSET_PX);
const PILL_RADIUS_PX = 22;

// iOS fidelity: the real San Francisco on Apple devices (the reference
// context), a near-equivalent neo-grotesque elsewhere. Brand Geist is
// deliberately not used here.
const IOS_FONT =
  '-apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", system-ui, sans-serif';

const INK = "oklch(1 0 0)";
const INK_DIM = "oklch(0.62 0 0)";
const CARD = "oklch(0.09 0 0)"; // TODO(audit): re-tune to Image #2
const TRAY = "oklch(0.928 0 0)";
const NODE_INK = "oklch(0 0 0)";

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

// Concentric squircle pair: light iOS tray wrapping the OLED Live-Activity
// card. Both are <Squircle> so the radius framework is the only corner source.
function FlightShell({ children }: { readonly children: ReactNode }) {
  return (
    <Squircle cornerRadius={TRAY_RADIUS_PX} style={{ background: TRAY, padding: TRAY_INSET_PX }}>
      <Squircle
        role="article"
        cornerRadius={CARD_RADIUS_PX}
        className="flex min-h-[15.5rem] flex-col justify-between px-9 py-8 sm:px-11 sm:py-9"
        style={{ background: CARD }}
      >
        {children}
      </Squircle>
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
    <p className="text-[0.82rem] font-medium tracking-[0.14em]" style={{ color: INK }}>
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
    <div className="flex flex-1 items-center py-5">
      <div className="flex w-full items-center gap-3 sm:gap-4">
        <Terminal label={source} />
        <Endpoint accent={accent}>
          <ArrowUpRight className="size-[1.15rem]" strokeWidth={2.75} />
        </Endpoint>
        <FlightArc progress={progress} accent={accent} phaseKind={phaseKind} />
        <Endpoint accent={accent}>
          <ArrowDownRight className="size-[1.15rem]" strokeWidth={2.75} />
        </Endpoint>
        <Terminal label={dest} />
      </div>
    </div>
  );
}

function Terminal({ label }: { readonly label: string }) {
  return (
    <span className="text-5xl font-extrabold tracking-tight sm:text-6xl" style={{ color: INK }}>
      {label}
    </span>
  );
}

function Endpoint({ accent, children }: { readonly accent: string; readonly children: ReactNode }) {
  return (
    <span
      className="flex size-9 shrink-0 items-center justify-center rounded-full"
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
      <p className="text-xl font-bold leading-tight" style={{ color: accent }}>
        {headline}
      </p>
      <p className="mt-1 text-[0.95rem] font-medium" style={{ color: accent, opacity: 0.82 }}>
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
      className="inline-flex shrink-0 items-center gap-2 px-4 py-2.5 text-lg font-bold transition-opacity hover:opacity-90"
      style={{ ...sq.style, background: "oklch(0.80 0.16 70)", color: NODE_INK }}
      to="/$orgSlug/runs/$providerRunId"
      params={{ orgSlug, providerRunId }}
      title="CI run logs"
    >
      <GitCommitGlyph className="size-[1.45rem]" />
      <span className="tabular-nums">{count}</span>
    </Link>
  );
}
