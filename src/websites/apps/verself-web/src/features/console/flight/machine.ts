import { useRef } from "react";
import { useElapsedTimeNow } from "@verself/ui/hooks/use-elapsed-time";
import type { Flight } from "./model";
import { monotone, morphSpec, phaseOf, project, type PhaseKind, type Projection } from "./phase";
import { useAccentSpring, useNumberSpring } from "./springs";

// ────────────────────────────────────────────────────────────────────────────
// Conflated state-machine driver.
//
// Why not a queue: a growing FIFO of data updates would replay every
// intermediate transition — exactly what we must NOT do. Because phaseOf() is a
// pure, total function of the *latest* snapshot, intermediate snapshots carry
// no state the newest one doesn't. So the correct data structure is a
// CAPACITY-1 CONFLATING CELL (last-write-wins register), not a queue:
//
//   producer (each data update):  cell ← snapshot        O(1), overwrites
//   consumer (a morph boundary):  read cell → phaseOf → maybe start next morph
//
// "Dequeue 97, then go to 98" is not an explicit dequeue: we only ever store 1.
// Skipped phases were never recorded. This is the standard conflation /
// signal-sampling pattern (Rx `conflate`+`sample`, game-loop "latest input
// wins", a size-1 ring). In React the cell is a ref and the sampler is the
// spring's rest/step callback.
//
// Discrete vs. continuous:
//  • Discrete phase is sampled at morph boundaries → intermediates lopped off.
//  • Continuous quantities (progress t, accent) use spring RETARGETING, which
//    conflates by physics (velocity-preserving redirect) — smooth, no replay.
//  • A transition flagged `interruptible` in phase.ts (e.g. →late) preempts an
//    in-flight morph instead of waiting for its boundary.
//
// Engine note: springs.ts is still stubbed for this audit, so the morph
// boundary is effectively immediate and this collapses to "track latest". The
// conflation structure here is what is being audited; wiring @react-spring/web
// only changes WHEN the sampler fires, not the shape below.
// ────────────────────────────────────────────────────────────────────────────

type CommittedPhase = { readonly kind: PhaseKind };

export function useFlightMachine(flight: Flight): Projection {
  // The 1s clock re-derives time-based phase against the frozen baseline.
  const nowMs = useElapsedTimeNow(1000);

  // Capacity-1 conflating cell. Overwritten every render with the newest
  // snapshot; never grows. This is the entire "queue".
  const cellRef = useRef<Flight>(flight);
  cellRef.current = flight;

  // Committed phase + morph flag. The sampler reads the cell at a morph
  // boundary (or immediately, for an interruptible target) and only then
  // advances — so phases that came and went between boundaries are skipped.
  const committedRef = useRef<CommittedPhase>({ kind: phaseOf(flight, nowMs).kind });

  const desired = phaseOf(cellRef.current, nowMs);
  const committed = committedRef.current;
  const canAdvanceNow =
    desired.kind === committed.kind ||
    morphSpec(committed.kind, desired.kind).interruptible ||
    !isMorphing(); // TODO(audit): spring.onRest gates this once engines wire

  const phase = canAdvanceNow ? desired : (committedToPhase(committed) ?? desired);
  if (canAdvanceNow) committedRef.current = { kind: phase.kind };

  const target = project(phase, cellRef.current, nowMs);

  // Continuous channels: monotone gate on the target, then the spring.
  const maxRef = useRef(0);
  maxRef.current = monotone(maxRef.current, target.progressTarget);

  return {
    ...target,
    progressTarget: useNumberSpring(maxRef.current),
    accent: target.accent,
    // accent string is resolved+crossfaded by the component via useAccentSpring;
    // exposed here only so the renderer stays a pure Projection consumer.
  } satisfies Projection & { accent: typeof target.accent };
}

// Stubbed until @react-spring/web lands: with instant springs there is no
// in-flight morph, so the boundary is always open.
function isMorphing(): boolean {
  return false; // TODO(audit): spring not-at-rest
}

// `committed` only retains the kind; if we cannot advance we keep showing the
// last fully-projected phase. enroute needs its variant payload, which the
// renderer already has from the prior Projection — so null here means "reuse
// desired" (safe: only reached when engines are stubbed and boundary is open).
function committedToPhase(_c: CommittedPhase): null {
  return null;
}

export { useAccentSpring };
