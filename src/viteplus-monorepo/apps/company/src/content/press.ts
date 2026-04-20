// Press. The brand kit is served from public/brand-kit/guardian-intelligence.zip.
// Contents: the four locked marks (Argent on Iron, Argent on Flare emboss,
// Argent on Paper chip, wordmark), tokens.css, and a copy of voice.md. A
// real PR asset kit replaces the stub when we ship printable material.

export const PRESS_META = {
  title: "Press — Guardian Intelligence",
  description:
    "Brand kit downloads, press contact, and guidance for writing about Guardian Intelligence.",
} as const;

export const press = {
  kicker: "For journalists and editors.",
  hero: "The Guardian brand, on the record.",
  intro:
    "Everything a reporter needs to write about Guardian Intelligence accurately. The kit is downloadable; the contact is a person, not a form.",
  kitHref: "/brand-kit/guardian-intelligence.zip",
  kitLabel: "Download the brand kit (.zip)",
  kitContents: [
    "Wordmark lockups in SVG — Iron, Paper, and Flare grounds.",
    "Wings mark in SVG — Argent on Iron and the chip/emboss carriers.",
    "Tokens file — ground hex values, typography stack.",
    "Voice guide — what we sound like and what we avoid.",
  ],
  contactLabel: "Press contact",
  contactEmail: "press@guardianintelligence.org",
  contactNote:
    "Please include the outlet, the angle, and the deadline. We answer by end of day Seattle time.",
  writingGuide: [
    'On first reference, write "Guardian Intelligence." On subsequent reference, "Guardian."',
    "The company is an American applied intelligence firm based in Seattle.",
    'Never write "founder" as "Founder" (capital F) outside of a title slot. It is a role, not a rank.',
  ],
} as const;
