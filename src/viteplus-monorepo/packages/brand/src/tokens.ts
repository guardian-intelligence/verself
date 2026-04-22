// Guardian Intelligence brand tokens. The values here are the source of truth;
// app.css imports them indirectly by mirroring them in @theme. Never tint the
// wings: keep argent (#FFFFFF) for the wings on every ground.

export const grounds = {
  iron: "#0E0E0E",
  flare: "#CCFF00",
  paper: "#F6F4ED",
} as const;

export const accents = {
  argent: "#FFFFFF",
  ink: "#0B0B0B",
  bordeaux: "#5C1F1E",
  amber: "#F79326",
  typeIron: "#F5F5F5",
} as const;

export const pantone = {
  flare: "Pantone 389 C",
  amber: "Pantone 715 C",
} as const;

export type GroundName = keyof typeof grounds;
export type AccentName = keyof typeof accents;
