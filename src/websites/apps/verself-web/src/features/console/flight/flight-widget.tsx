import type { ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { useElapsedTimeNow } from "@verself/ui/hooks/use-elapsed-time";
import { cn } from "@verself/ui/lib/utils";
import { ArrowDownRight, ArrowUpRight, Plane } from "lucide-react";
import { GitCommitGlyph } from "./git-commit-glyph";
import { formatElapsed, isRunningLate, type Flight } from "./model";

export function FlightBoard({
  flights,
  orgSlug,
}: {
  readonly flights: readonly Flight[];
  readonly orgSlug: string;
}) {
  if (flights.length === 0) {
    return (
      <FlightCanvas>
        <p className="text-center text-sm text-muted-foreground">No CI in flight</p>
      </FlightCanvas>
    );
  }
  return (
    <FlightCanvas>
      <div className="grid gap-4 md:grid-cols-2">
        {flights.map((flight) => (
          <FlightCard key={flight.key} flight={flight} orgSlug={orgSlug} />
        ))}
      </div>
    </FlightCanvas>
  );
}

export function FlightConsoleSkeleton() {
  return (
    <FlightCanvas>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="h-44 animate-pulse rounded-2xl bg-zinc-900 ring-1 ring-white/10" />
      </div>
    </FlightCanvas>
  );
}

function FlightCanvas({ children }: { readonly children: ReactNode }) {
  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-10 md:py-16">
      <div className="flex min-h-[14rem] flex-col justify-center">{children}</div>
    </div>
  );
}

function FlightCard({ flight, orgSlug }: { readonly flight: Flight; readonly orgSlug: string }) {
  // Local 1s clock from the shell-mounted ElapsedTimeProvider — the only
  // liveness ticker. The Electric shape delivers state changes; the clock just
  // re-renders elapsed against the frozen baseline. No polling anywhere.
  const nowMs = useElapsedTimeNow(1000);
  const late = isRunningLate(flight, nowMs);

  return (
    <article className="relative overflow-hidden rounded-2xl bg-zinc-950 px-6 py-5 text-white ring-1 ring-white/10">
      <p className="text-xs font-semibold tracking-[0.2em] text-zinc-400">{flight.actorLabel}</p>

      <div className="mt-3 flex items-center gap-3">
        <span className="text-3xl font-extrabold tracking-tight">{flight.sourceLabel}</span>
        <FlightArc late={late} />
        <span className="text-3xl font-extrabold tracking-tight">{flight.destLabel}</span>
      </div>

      <div className="mt-4 flex items-end justify-between gap-3">
        <div className="min-w-0">
          <p className={cn("text-sm font-semibold", late ? "text-amber-400" : "text-emerald-400")}>
            {late ? "Running Late" : "On Time"}
          </p>
          <p className="mt-0.5 text-sm text-zinc-300">
            {flight.statusLabel} · {formatElapsed(nowMs - flight.departedAtMs)}
          </p>
          <p className="text-sm text-zinc-500">
            {flight.jobsLeft} {flight.jobsLeft === 1 ? "job" : "jobs"} left
          </p>
        </div>
        {flight.commitPill !== null ? (
          <Link
            to="/$orgSlug/runs/$providerRunId"
            params={{ orgSlug, providerRunId: flight.providerRunId }}
            className="flex shrink-0 items-center gap-1.5 rounded-xl bg-amber-400 px-3 py-2 text-sm font-bold text-zinc-950 transition-opacity hover:opacity-90"
            title="CI run logs"
          >
            <GitCommitGlyph className="size-4" />
            {flight.commitPill}
          </Link>
        ) : null}
      </div>
    </article>
  );
}

// Decorative flight motif. Liveness is the clock + badge, not arc geometry —
// keeping this static avoids fragile progress math and matches the mock.
function FlightArc({ late }: { readonly late: boolean }) {
  const stroke = late ? "#fbbf24" : "#34d399";
  return (
    <div className="relative flex flex-1 items-center" aria-hidden="true">
      <Endpoint>
        <ArrowUpRight className="size-3.5" style={{ color: stroke }} />
      </Endpoint>
      <svg viewBox="0 0 120 40" preserveAspectRatio="none" className="h-9 flex-1" fill="none">
        <path d="M2 34 Q 60 -4 92 16" stroke={stroke} strokeWidth="3" strokeLinecap="round" />
        <path
          d="M92 16 Q 108 26 118 30"
          stroke={stroke}
          strokeWidth="3"
          strokeLinecap="round"
          strokeDasharray="2 6"
          opacity="0.6"
        />
      </svg>
      <Plane
        className="absolute left-[62%] top-0 size-4 -translate-y-0.5 rotate-45"
        style={{ color: stroke }}
      />
      <Endpoint>
        <ArrowDownRight className="size-3.5" style={{ color: stroke }} />
      </Endpoint>
    </div>
  );
}

function Endpoint({ children }: { readonly children: ReactNode }) {
  return (
    <span className="flex size-6 items-center justify-center rounded-full bg-white/10">
      {children}
    </span>
  );
}
