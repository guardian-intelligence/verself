import { describe, expect, it } from "vite-plus/test";
import * as fc from "fast-check";

import {
  anchoredSheetReducer,
  type SheetEvent,
  type SheetPhase,
  type SheetState,
} from "@verself/ui/components/ui/anchored-sheet";

const dragMoveEventArb: fc.Arbitrary<SheetEvent> = fc.record({
  type: fc.constant("DRAG_MOVE"),
  dragY: fc.integer({ max: 1200, min: 0 }),
});

const dismissEventArb: fc.Arbitrary<SheetEvent> = fc.record({
  type: fc.constant("DISMISS"),
  dragY: fc.integer({ max: 1200, min: 0 }),
});

const sheetEventArb: fc.Arbitrary<SheetEvent> = fc.oneof(
  dragMoveEventArb,
  dismissEventArb,
  fc.constant({ type: "PRESENT" } as SheetEvent),
  fc.constant({ type: "MEASURED" } as SheetEvent),
  fc.constant({ type: "SPRING_SETTLED" } as SheetEvent),
  fc.constant({ type: "DRAG_START" } as SheetEvent),
  fc.constant({ type: "RESTORE" } as SheetEvent),
);

const sheetStateArb: fc.Arbitrary<SheetState> = fc.record({
  dragY: fc.integer({ min: 0, max: 1200 }),
  phase: fc.constantFrom(
    "hidden",
    "measuring",
    "presenting",
    "presented",
    "dragging",
    "restoring",
    "dismissing",
  ) satisfies fc.Arbitrary<SheetPhase>,
});

describe("anchored-sheet reducer", () => {
  it("always transitions hidden + PRESENT to measuring", () => {
    fc.assert(
      fc.property(sheetStateArb, fc.array(sheetEventArb, { minLength: 1, maxLength: 12 }), (seed, events) => {
        let state: SheetState = seed.phase === "hidden" ? { ...seed, dragY: seed.dragY } : seed;

        for (const event of events) {
          const previous = state;
          const next = anchoredSheetReducer(previous, event);

          if (previous.phase === "hidden" && event.type === "PRESENT") {
            expect(next).toEqual({ dragY: 0, phase: "measuring" });
          }
          if (previous.phase === "measuring" && event.type === "MEASURED") {
            expect(next).toEqual({ dragY: 0, phase: "presenting" });
          }

          if (previous.phase === "dragging" && event.type === "DRAG_MOVE") {
            expect(next.dragY).toBe(event.dragY);
          } else if (event.type === "DRAG_MOVE") {
            expect(next.dragY).toBe(previous.dragY);
          }

          state = next;
        }
      }),
      { numRuns: 128 },
    );
  });

  it("respects terminal outcomes for SPRING_SETTLED", () => {
    fc.assert(
      fc.property(
        fc.array(
          fc.record({
            phase: fc.constantFrom("presenting", "restoring", "dismissing", "presented", "dragging", "hidden"),
            dragY: fc.integer({ min: 0, max: 1200 }),
          }) as fc.Arbitrary<SheetState>,
          { minLength: 1, maxLength: 20 },
        ),
        (states) => {
          for (const state of states) {
            const next = anchoredSheetReducer(state, { type: "SPRING_SETTLED" });
            if (state.phase === "presenting" || state.phase === "restoring") {
              expect(next.phase).toBe("presented");
              expect(next.dragY).toBe(0);
            } else if (state.phase === "dismissing") {
              expect(next.phase).toBe("hidden");
              expect(next.dragY).toBe(0);
            } else {
              expect(next).toEqual(state);
            }
          }
        },
      ),
      { numRuns: 24 },
    );
  });

  it("stays visible right after a hidden->PRESENT handoff", () => {
    fc.assert(
      fc.property(fc.array(sheetEventArb, { minLength: 1, maxLength: 20 }), (events) => {
        let state: SheetState = { dragY: 112, phase: "hidden" };
        let activeVisibleIntent = false;

        for (const event of events) {
          const next = anchoredSheetReducer(state, event);

          if (state.phase === "hidden" && event.type === "PRESENT") {
            activeVisibleIntent = true;
            expect(next).toEqual({ dragY: 0, phase: "measuring" });
          }

          if (state.phase === "dismissing" && event.type === "SPRING_SETTLED" && next.phase === "hidden") {
            activeVisibleIntent = false;
          }

          if (activeVisibleIntent) {
            expect(next.phase).not.toBe("hidden");
          }

          state = next;
        }
      }),
      { numRuns: 256 },
    );
  });
});
