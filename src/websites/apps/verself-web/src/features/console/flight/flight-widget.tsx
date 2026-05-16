import type { CSSProperties, ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useElapsedTimeNow } from "@verself/ui/hooks/use-elapsed-time";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { GitCommitGlyph } from "./git-commit-glyph";
import { formatRemaining, isRunningLate, remainingMs, type Flight } from "./model";

// OLED-accurate oklch palette (no sRGB hex anywhere in the widget). Tuned to
// the iOS Live Activity reference: near-black card, bright system green,
// Flighty amber pill.
const C = {
  frame: "oklch(0.928 0 0)",
  card: "oklch(0.16 0 0)",
  ink: "oklch(1 0 0)",
  inkDim: "oklch(0.62 0 0)",
  green: "oklch(0.82 0.19 150)",
  greenDim: "oklch(0.72 0.14 150)",
  amber: "oklch(0.83 0.152 82)",
  amberInk: "oklch(0.22 0.03 72)",
  lateInk: "oklch(0.80 0.16 70)",
  lateDim: "oklch(0.72 0.13 70)",
  black: "oklch(0 0 0)",
} as const;

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
            <p className="text-sm font-medium" style={{ color: C.inkDim }}>
              No CI in flight
            </p>
          </div>
        </FlightShell>
      ) : (
        flights.map((flight) => (
          <FlightCard key={flight.key} flight={flight} orgSlug={orgSlug} />
        ))
      )}
    </FlightCanvas>
  );
}

export function FlightConsoleSkeleton() {
  return (
    <FlightCanvas>
      <FlightShell>
        <div className="flex-1 animate-pulse rounded-[1.5rem]" style={{ background: "oklch(0.22 0 0)" }} />
      </FlightShell>
    </FlightCanvas>
  );
}

// Width clamp: never edge-to-edge on phones (px gutter), never cramped on wide
// screens (a generous fixed widget size, centred). Vertically centred in the
// console viewport.
function FlightCanvas({ children }: { readonly children: ReactNode }) {
  return (
    <div className="flex min-h-[80svh] w-full items-center justify-center px-5 py-12">
      <div className="flex w-full max-w-[42rem] flex-col gap-5">{children}</div>
    </div>
  );
}

// iOS widget framing: a light continuous-radius container with the near-black
// Live-Activity card inset inside it (matches the reference exactly).
function FlightShell({ children }: { readonly children: ReactNode }) {
  return (
    <div
      className="rounded-[2.6rem] p-[7px] shadow-[0_18px_50px_-20px_oklch(0_0_0/0.5)]"
      style={{ background: C.frame }}
    >
      <article
        className="flex min-h-[15.5rem] flex-col justify-between rounded-[2.15rem] px-9 py-8 sm:px-11 sm:py-9"
        style={{ background: C.card }}
      >
        {children}
      </article>
    </div>
  );
}

function FlightCard({ flight, orgSlug }: { readonly flight: Flight; readonly orgSlug: string }) {
  // Local 1s clock from the shell-mounted ElapsedTimeProvider — the only
  // liveness ticker. Electric delivers state; the clock re-renders the ETA
  // against the frozen baseline. No polling.
  const nowMs = useElapsedTimeNow(1000);
  const late = isRunningLate(flight, nowMs);
  const remaining = remainingMs(flight, nowMs);
  const accent = late ? C.lateInk : C.green;
  const accentDim = late ? C.lateDim : C.greenDim;
  const etaText =
    late || remaining === null
      ? flight.statusLabel
      : `${flight.statusLabel} · ${formatRemaining(remaining)}`;

  return (
    <FlightShell>
      <p
        className="text-[0.82rem] font-medium tracking-[0.14em]"
        style={{ color: C.ink }}
      >
        {flight.actorLabel}
      </p>

      <div className="flex flex-1 items-center py-5">
        <div className="flex w-full items-center gap-3 sm:gap-4">
          <span
            className="text-5xl font-extrabold tracking-tight sm:text-6xl"
            style={{ color: C.ink }}
          >
            {flight.sourceLabel}
          </span>
          <Endpoint color={accent}>
            <ArrowUpRight className="size-[1.15rem]" strokeWidth={2.75} />
          </Endpoint>
          <FlightArc color={accent} />
          <Endpoint color={accent}>
            <ArrowDownRight className="size-[1.15rem]" strokeWidth={2.75} />
          </Endpoint>
          <span
            className="text-5xl font-extrabold tracking-tight sm:text-6xl"
            style={{ color: C.ink }}
          >
            {flight.destLabel}
          </span>
        </div>
      </div>

      <div className="flex items-end justify-between gap-4">
        <div className="min-w-0">
          <p className="text-xl font-bold leading-tight" style={{ color: accent }}>
            {late ? "Running Late" : "On Time"}
          </p>
          <p className="mt-1 text-[0.95rem] font-medium" style={{ color: accentDim }}>
            {etaText}
          </p>
        </div>
        {flight.commitPill !== null ? (
          <Link
            to="/$orgSlug/runs/$providerRunId"
            params={{ orgSlug, providerRunId: flight.providerRunId }}
            className="inline-flex shrink-0 items-center gap-2 rounded-[1.15rem] px-4 py-2.5 text-lg font-bold transition-opacity hover:opacity-90"
            style={{ background: C.amber, color: C.amberInk }}
            title="CI run logs"
          >
            <GitCommitGlyph className="size-[1.45rem]" />
            <span className="tabular-nums">{flight.commitPill}</span>
          </Link>
        ) : null}
      </div>
    </FlightShell>
  );
}

function Endpoint({ color, children }: { readonly color: string; readonly children: ReactNode }) {
  return (
    <span
      className="flex size-9 shrink-0 items-center justify-center rounded-full"
      style={{ background: color, color: C.black }}
    >
      {children}
    </span>
  );
}

// Decorative flight motif: bold solid arc → white triangle marker → dotted
// tail into the destination. Static; liveness is the badge + ETA, not arc
// geometry (avoids fragile progress math, matches the reference).
function FlightArc({ color }: { readonly color: string }) {
  const triangle: CSSProperties = {
    color: C.ink,
    left: "58%",
    top: "8%",
    transform: "rotate(20deg)",
  };
  return (
    <div className="relative flex h-12 min-w-0 flex-1 items-center" aria-hidden="true">
      <svg
        viewBox="0 0 200 70"
        preserveAspectRatio="none"
        className="h-full w-full"
        fill="none"
      >
        <path
          d="M4 54 Q 72 -8 132 26"
          stroke={color}
          strokeWidth="5"
          strokeLinecap="round"
        />
        <path
          d="M140 30 Q 168 42 196 54"
          stroke={color}
          strokeWidth="5"
          strokeLinecap="round"
          strokeDasharray="1.5 9"
        />
      </svg>
      <svg viewBox="0 0 12 12" className="absolute size-[1.05rem]" style={triangle}>
        <polygon points="2,1.5 11,6 2,10.5" fill="currentColor" />
      </svg>
    </div>
  );
}
