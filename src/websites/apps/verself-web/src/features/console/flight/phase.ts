import type { Flight } from "./model";

// ────────────────────────────────────────────────────────────────────────────
// The widget as an EXTENSIBLE state machine.
//
//   data update ─► classify ─► Phase ─► project ─► Projection ─► springs morph
//
// Extensibility contract (open/closed):
//  • Add a phase  = add a `Phase` variant + one `CLASSIFIERS` row + one
//                   `project` arm. Existing rows are not touched.
//  • The renderer NEVER switches on Phase. It consumes a flat `Projection`
//    (animated targets), so a new phase changes values, not components.
//  • Transition behavior is declared in `MORPH`, a (from→to) table, not
//    branched in code — so morph rules are auditable in one place.
//
// We track only what the product tracks: elapsed vs. the repo's historical p50
// for the critical-path job. No cold / diverted / landed phase.
// ────────────────────────────────────────────────────────────────────────────

export type Phase =
  | { readonly kind: "boarding" }
  | { readonly kind: "enroute"; readonly remainingMs: number | null }
  | { readonly kind: "late" };

export type PhaseKind = Phase["kind"];

export type AccentToken = "accent-green" | "accent-amber";

// What the view binds to. Every field is independently animatable; adding a
// phase produces different values here, never new render branches.
export type Projection = {
  readonly phaseKind: PhaseKind;
  readonly progressTarget: number; // pre-monotone; the machine gates it
  readonly accent: AccentToken;
  readonly headline: string;
  readonly detail: string;
};

export const LATE_ASYMPTOTE = 0.92;
const INDETERMINATE_DRIFT = 0.06; // TODO(audit): no-baseline arc treatment

function isBuilding(f: Flight): boolean {
  return f.statusLabel !== "Queued" && f.statusLabel !== "Waiting";
}

// Ordered classifiers: first match wins. New phases insert a row; nothing else
// changes. Each guard is pure and total.
const CLASSIFIERS: ReadonlyArray<(f: Flight, now: number) => Phase | null> = [
  (f) => (isBuilding(f) ? null : { kind: "boarding" }),
  (f, now) =>
    f.baselineMs !== null && now - f.departedAtMs > f.baselineMs ? { kind: "late" } : null,
  (f, now) => ({
    kind: "enroute",
    remainingMs: f.baselineMs === null ? null : f.baselineMs - (now - f.departedAtMs),
  }),
];

export function phaseOf(f: Flight, now: number): Phase {
  for (const classify of CLASSIFIERS) {
    const phase = classify(f, now);
    if (phase) return phase;
  }
  // CLASSIFIERS' last row is total, so this is unreachable by construction.
  return { kind: "enroute", remainingMs: null };
}

// (from→to) morph rules. `interruptible: true` lets that transition preempt an
// in-flight morph instead of waiting for the morph boundary (see machine.ts) —
// e.g. crossing into `late` should not be starved behind a long enroute morph.
// Unlisted pairs use the default (wait for the boundary, then morph).
export type MorphSpec = { readonly interruptible: boolean };
const DEFAULT_MORPH: MorphSpec = { interruptible: false };
const MORPH = new Map<string, MorphSpec>([
  ["enroute→late", { interruptible: true }],
  ["boarding→late", { interruptible: true }],
]);
export function morphSpec(from: PhaseKind, to: PhaseKind): MorphSpec {
  return MORPH.get(`${from}→${to}`) ?? DEFAULT_MORPH;
}

// Phase → flat animated targets. The single place phase semantics become
// pixels. boarding pins 0; late holds the asymptote; enroute tracks elapsed.
export function project(phase: Phase, f: Flight, now: number): Projection {
  switch (phase.kind) {
    case "boarding":
      return {
        phaseKind: "boarding",
        progressTarget: 0,
        accent: "accent-green",
        headline: "On Time",
        detail: f.statusLabel,
      };
    case "late":
      return {
        phaseKind: "late",
        progressTarget: LATE_ASYMPTOTE,
        accent: "accent-amber",
        headline: "Running Late",
        detail: f.statusLabel,
      };
    case "enroute": {
      const raw =
        f.baselineMs === null
          ? INDETERMINATE_DRIFT
          : Math.min(Math.max((now - f.departedAtMs) / f.baselineMs, 0), LATE_ASYMPTOTE);
      const detail =
        phase.remainingMs === null
          ? f.statusLabel
          : `${f.statusLabel} in ${formatRemaining(phase.remainingMs)}`;
      return {
        phaseKind: "enroute",
        progressTarget: raw,
        accent: "accent-green",
        headline: "On Time",
        detail,
      };
    }
  }
}

// The monotone gate: the single place "never backwards" is enforced. The
// machine threads the running max through; the spring only eases toward a
// non-decreasing target.
export function monotone(prevMax: number, candidate: number): number {
  return candidate > prevMax ? candidate : prevMax;
}

// Minute resolution, compact `Nm` per the brief ("Building in 55m"):
// "<1m" floor, ">60m" ceiling.
export function formatRemaining(ms: number): string {
  if (ms < 60_000) return "<1m";
  const minutes = Math.round(ms / 60_000);
  if (minutes > 60) return ">60m";
  return `${minutes}m`;
}
