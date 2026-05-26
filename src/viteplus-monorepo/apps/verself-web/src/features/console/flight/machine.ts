import { useRef } from "react";
import { useElapsedTimeNow } from "@verself/ui/hooks/use-elapsed-time";
import type { Flight } from "./model";
import { monotone, morphSpec, phaseOf, project, type Phase, type Projection } from "./phase";
import { useSpring } from "./springs";

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
//   consumer (a morph boundary):  read cell → phaseOf → maybe advance
//
// "Dequeue 97, then go to 98" is not an explicit dequeue: we only ever store 1;
// skipped phases were never recorded. Standard conflation / signal-sampling
// (Rx conflate+sample, game-loop "latest input wins", a size-1 ring). Here the
// cell is a ref and the morph boundary is the progress spring reaching rest.
//
// Discrete vs. continuous:
//  • Discrete phase advances only at a morph boundary (spring at rest) unless
//    the (from→to) is `interruptible` (e.g. →late) — intermediates lopped.
//  • Continuous quantities (progress, accent) ride spring retargeting, which
//    conflates by physics (velocity-preserving redirect) — smooth, no replay.
//  • Monotonicity is enforced on the target only; the spring eases toward a
//    non-decreasing value, so the marker is structurally unable to reverse.
// ────────────────────────────────────────────────────────────────────────────

export function useFlightMachine(flight: Flight): Projection {
  // The 1s clock re-derives time-based phase against the frozen baseline.
  const nowMs = useElapsedTimeNow(1000);

  // Capacity-1 conflating cell. Overwritten every render with the newest
  // snapshot; never grows. This is the entire "queue".
  const cellRef = useRef<Flight>(flight);
  cellRef.current = flight;

  // Committed phase + the previous frame's spring-rest reading (the morph
  // boundary). Reading last frame's rest avoids a phase→target→spring cycle.
  const committedRef = useRef<Phase>(phaseOf(flight, nowMs));
  const restRef = useRef(true);

  const desired = phaseOf(cellRef.current, nowMs);
  const committed = committedRef.current;
  const canAdvance =
    desired.kind === committed.kind ||
    morphSpec(committed.kind, desired.kind).interruptible ||
    restRef.current;
  const phase = canAdvance ? desired : committed;
  committedRef.current = phase;

  const target = project(phase, cellRef.current, nowMs);

  // Continuous channel: monotone gate on the target, then the spring.
  const maxRef = useRef(0);
  maxRef.current = monotone(maxRef.current, target.progressTarget);
  const progress = useSpring(maxRef.current);
  restRef.current = progress.atRest;

  return { ...target, progressTarget: progress.value };
}
