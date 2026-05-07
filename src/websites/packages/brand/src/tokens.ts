// Guardian Intelligence brand tokens. The values here are the source of truth;
// app.css imports them indirectly by mirroring them in @theme. Never tint the
// wings: keep argent (#FFFFFF) for the wings on every ground.

export const grounds = {
  iron: "#0E0E0E",
  flare: "#CCFF00",
  paper: "#F6F4ED",
  // Vellum is a same-family inset of Paper — a slightly darker, warmer
  // parchment used for Letters colophons and spec blocks so they read as a
  // quiet recess rather than a slab of contrast.
  vellum: "#E8E2D2",
} as const;

export const accents = {
  argent: "#FFFFFF",
  ink: "#0B0B0B",
  bordeaux: "#5C1F1E",
  amber: "#F79326",
  typeIron: "#F5F5F5",
} as const;

// Muted-type families. Each is an opacity ramp (strong / default / meta /
// faint) tuned to hold WCAG AA (≥ 4.5:1) on its ground for small text. The
// base rgb is derived from the ground's type colour — argent on Iron, ink on
// Paper. In CSS these appear as --color-ash-* / --color-stone-* (reachable as
// `text-ash`, `text-stone-meta`, etc.) and are consumed indirectly through
// --treatment-muted-* which resolves to the correct family per data-treatment.
export const mutedFamilies = {
  // Iron-muted type. Argent with opacity, reading as cool grey on dark.
  ash: {
    base: "245, 245, 245",
    strong: 0.82,
    default: 0.72,
    meta: 0.6,
    faint: 0.55,
  },
  // Paper-muted ink. Ink with opacity, reading as warm grey on paper.
  stone: {
    base: "11, 11, 11",
    strong: 0.78,
    default: 0.7,
    meta: 0.6,
    faint: 0.55,
  },
} as const;

export const pantone = {
  flare: "Pantone 389 C",
  bordeaux: "Pantone 504 C",
  amber: "Pantone 715 C",
} as const;

export type GroundName = keyof typeof grounds;
export type AccentName = keyof typeof accents;
export type MutedFamilyName = keyof typeof mutedFamilies;
