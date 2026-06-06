export const guardianGlowMetricIds = {
  draws: "guardian-glow-draws-value",
  dpr: "guardian-glow-dpr-value",
  fps: "guardian-glow-fps-value",
  frame: "guardian-glow-frame-value",
  pixels: "guardian-glow-pixels-value",
  probeCount: "guardian-glow-probe-count-value",
  probeMs: "guardian-glow-probe-ms-value",
  probeRate: "guardian-glow-probe-rate-value",
  probeRes: "guardian-glow-probe-res-value",
  programs: "guardian-glow-programs-value",
  triangles: "guardian-glow-triangles-value",
} as const;

export type GuardianGlowMetricId =
  (typeof guardianGlowMetricIds)[keyof typeof guardianGlowMetricIds];

export function setGuardianGlowMetric(id: GuardianGlowMetricId, value: string) {
  document.getElementById(id)?.replaceChildren(value);
}
