import type { CSSProperties } from "react";

type CriticalTreatment = "workshop" | "newsroom" | "letters";

const CRITICAL_TREATMENTS = {
  workshop: {
    ground: "#0e0e0e",
    ink: "#f5f5f5",
    colorScheme: "dark",
  },
  newsroom: {
    ground: "#ffffff",
    ink: "#0b0b0b",
    colorScheme: "light",
  },
  letters: {
    ground: "#fff4dc",
    ink: "#0b0b0b",
    colorScheme: "light",
  },
} as const satisfies Record<
  CriticalTreatment,
  { readonly ground: string; readonly ink: string; readonly colorScheme: "dark" | "light" }
>;

export function criticalTreatmentHead(treatment: CriticalTreatment, criticalCss: string) {
  const { ground } = CRITICAL_TREATMENTS[treatment];

  return {
    meta: [{ name: "theme-color", content: ground }],
    styles: [{ children: criticalCss }],
  };
}

export function criticalTreatmentRootStyle(treatment: CriticalTreatment): CSSProperties {
  const { ground, ink } = CRITICAL_TREATMENTS[treatment];

  return {
    minHeight: "100svh",
    display: "flex",
    flexDirection: "column",
    backgroundColor: `var(--treatment-ground, ${ground})`,
    color: `var(--treatment-ink, ${ink})`,
  };
}
