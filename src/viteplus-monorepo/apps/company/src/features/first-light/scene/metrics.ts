import type { ArrivalFrameMetrics } from "../types";

export function arrivalFrameMetrics(samples: readonly number[]): ArrivalFrameMetrics {
  if (samples.length === 0) {
    return { p50: 0, p99: 0, samples: 0 };
  }
  const sorted = [...samples].sort((a, b) => a - b);
  return {
    p50: percentile(sorted, 0.5),
    p99: percentile(sorted, 0.99),
    samples: sorted.length,
  };
}

function percentile(sorted: readonly number[], p: number): number {
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * p) - 1));
  const value = sorted[index] ?? sorted[sorted.length - 1] ?? 0;
  return Number(value.toFixed(2));
}
