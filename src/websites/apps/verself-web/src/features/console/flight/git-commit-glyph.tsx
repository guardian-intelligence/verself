import type { SVGProps } from "react";

// The commit glyph: a solid node between two short, heavy connector bars — our
// iconography analogue of the reference's baggage chip. The viewBox is
// tight-cropped to the content (no internal dead margin), the node is large
// relative to the box and the connectors are short and thick, so it reads as
// one compact, dense mark rather than a thin graph fragment — ≈20% less
// internal negative space than the prior pass (brief note d). It carries an
// intrinsic ~1.5:1 aspect via the viewBox; the caller sets height only and the
// width follows, so it stays the reference baggage-chip footprint at any
// scale. `currentColor` so the pill paints it solid NODE_INK on the gold.
export function GitCommitGlyph(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 88 56" fill="currentColor" aria-hidden="true" {...props}>
      <rect x="0" y="19" width="24" height="18" rx="9" />
      <rect x="64" y="19" width="24" height="18" rx="9" />
      <circle cx="44" cy="28" r="19" />
    </svg>
  );
}
