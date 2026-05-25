export interface FixedTimestepAdvance {
  readonly droppedSeconds: number;
  readonly interpolationAlpha: number;
  readonly stepSeconds: number;
  readonly steps: number;
}

interface FixedTimestepOptions {
  readonly maxFrameSeconds: number;
  readonly maxSubSteps: number;
  readonly stepSeconds: number;
}

export function createFixedTimestepLoop(options: FixedTimestepOptions) {
  let accumulator = 0;

  return {
    advance(frameDeltaSeconds: number, active: boolean): FixedTimestepAdvance {
      if (!active) {
        accumulator = 0;
        return {
          droppedSeconds: 0,
          interpolationAlpha: 0,
          stepSeconds: options.stepSeconds,
          steps: 0,
        };
      }

      const clampedDelta = Math.min(Math.max(frameDeltaSeconds, 0), options.maxFrameSeconds);
      accumulator += clampedDelta;

      let steps = 0;
      while (accumulator >= options.stepSeconds && steps < options.maxSubSteps) {
        accumulator -= options.stepSeconds;
        steps += 1;
      }

      const droppedSeconds = accumulator >= options.stepSeconds ? accumulator : 0;
      if (droppedSeconds > 0) {
        accumulator = 0;
      }

      return {
        droppedSeconds,
        interpolationAlpha: accumulator / options.stepSeconds,
        stepSeconds: options.stepSeconds,
        steps,
      };
    },
  };
}
