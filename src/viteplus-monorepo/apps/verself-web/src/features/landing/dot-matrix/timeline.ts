export interface DotMatrixTimelineSample {
  readonly ambient: number;
  readonly contrast: number;
  readonly ignition: number;
  readonly phase: number;
}

interface TimelineKnot {
  readonly time: number;
  readonly value: number;
}

interface TimelineChannel {
  readonly clamp?: readonly [number, number];
  readonly knots: ReadonlyArray<TimelineKnot>;
  readonly tailSlope?: number;
}

const dotMatrixTimeline = {
  ambient: {
    clamp: [0, 1.2],
    knots: [
      { time: 0, value: 0.12 },
      { time: 0.55, value: 0.72 },
      { time: 1.4, value: 1 },
      { time: 2.8, value: 0.84 },
      { time: 4.8, value: 0.78 },
    ],
  },
  contrast: {
    clamp: [0.6, 1.3],
    knots: [
      { time: 0, value: 0.82 },
      { time: 0.8, value: 1.14 },
      { time: 1.8, value: 1.04 },
      { time: 4.8, value: 1 },
    ],
  },
  ignition: {
    clamp: [0, 1],
    knots: [
      { time: 0, value: 0.22 },
      { time: 0.18, value: 1 },
      { time: 1.2, value: 0.94 },
      { time: 2.2, value: 0.26 },
      { time: 3.25, value: 0 },
    ],
  },
  phase: {
    knots: [
      { time: 0, value: 0 },
      { time: 0.35, value: 0.322 },
      { time: 0.85, value: 0.784 },
      { time: 1.55, value: 1.386 },
      { time: 2.8, value: 2.254 },
      { time: 4.8, value: 3.346 },
    ],
    tailSlope: 0.504,
  },
} satisfies Record<keyof DotMatrixTimelineSample, TimelineChannel>;

export function sampleDotMatrixTimeline(elapsedSeconds: number): DotMatrixTimelineSample {
  const time = Math.max(0, elapsedSeconds);
  return {
    ambient: sampleTimelineChannel(dotMatrixTimeline.ambient, time),
    contrast: sampleTimelineChannel(dotMatrixTimeline.contrast, time),
    ignition: sampleTimelineChannel(dotMatrixTimeline.ignition, time),
    phase: sampleTimelineChannel(dotMatrixTimeline.phase, time),
  };
}

function sampleTimelineChannel(channel: TimelineChannel, time: number): number {
  const knots = channel.knots;
  if (knots.length === 0) {
    throw new Error("timeline channel requires at least one knot");
  }

  const first = knots[0];
  if (first === undefined) {
    throw new Error("timeline channel requires at least one knot");
  }
  if (time <= first.time) {
    return clamp(channel, first.value);
  }

  const last = knots.at(-1);
  if (last === undefined) {
    throw new Error("timeline channel requires at least one knot");
  }
  if (time >= last.time) {
    return clamp(channel, last.value + (time - last.time) * (channel.tailSlope ?? 0));
  }

  for (let index = 0; index < knots.length - 1; index += 1) {
    const current = knots[index];
    const next = knots[index + 1];
    if (current === undefined || next === undefined) continue;
    if (time >= current.time && time <= next.time) {
      return clamp(channel, sampleHermite(knots, index, time));
    }
  }

  return clamp(channel, last.value);
}

function sampleHermite(knots: ReadonlyArray<TimelineKnot>, index: number, time: number): number {
  const current = knots[index];
  const next = knots[index + 1];
  if (current === undefined || next === undefined) {
    throw new Error("invalid timeline segment");
  }

  const previous = knots[index - 1] ?? current;
  const following = knots[index + 2] ?? next;
  const duration = next.time - current.time;
  if (duration <= 0) {
    throw new Error("timeline knot times must be strictly increasing");
  }

  const localTime = (time - current.time) / duration;
  const localTime2 = localTime * localTime;
  const localTime3 = localTime2 * localTime;
  const tangentA = finiteSlope(previous, next) * duration;
  const tangentB = finiteSlope(current, following) * duration;

  return (
    (2 * localTime3 - 3 * localTime2 + 1) * current.value +
    (localTime3 - 2 * localTime2 + localTime) * tangentA +
    (-2 * localTime3 + 3 * localTime2) * next.value +
    (localTime3 - localTime2) * tangentB
  );
}

function finiteSlope(a: TimelineKnot, b: TimelineKnot): number {
  const duration = b.time - a.time;
  return duration > 0 ? (b.value - a.value) / duration : 0;
}

function clamp(channel: TimelineChannel, value: number): number {
  const range = channel.clamp;
  if (range === undefined) return value;
  return Math.min(Math.max(value, range[0]), range[1]);
}
