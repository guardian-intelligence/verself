import type { SVGProps } from "react";

// The prod commit glyph, kept — but the center node is now a SOLID disc
// (was a hollow ring) and the connector bar is heavier/thicker, per brief.
// currentColor so the amber pill can paint it true black.
export function GitCommitGlyph(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true" {...props}>
      <rect x="6" y="110" width="74" height="36" rx="18" />
      <rect x="176" y="110" width="74" height="36" rx="18" />
      <circle cx="128" cy="128" r="62" />
    </svg>
  );
}
