import type { SVGProps } from "react";

// The wings path is the source-of-truth glyph for Guardian Intelligence.
// Coordinates are deliberately preserved verbatim from the typographic playground
// (logo-playground.html) — small adjustments here propagate to favicons, embosses,
// chips, OG cards, and email signatures, so changes need brand sign-off.
export const WINGS_PATH_D =
  "M124.497 236.057L226.107 116.237L225.816 161.189C222.205 165.295 221.465 170.585 204.635 182.995C187.815 195.405 138.065 227.365 124.497 236.057ZM130.031 237.06C142.891 235.13 191.081 235.09 207.191 225.49C223.291 215.9 223.421 187.17 226.671 179.5C219.181 185.187 197.671 203.647 181.671 213.117C165.671 222.587 139.191 232.527 130.031 237.06Z";

export const WINGS_TIGHT_VIEWBOX = "105 106 140 140";
export const WINGS_PADDED_VIEWBOX = "30 31 292 292";
// Glyph-hugging viewBox: the smallest box that contains the wings path exactly.
// Derived from the extrema of WINGS_PATH_D (minX 124.497, minY 116.237, width
// 102.174, height 120.823). Used by the Lockup so the SVG bottom coincides
// with the lower wing's visual tip — wordmarks bottom-align against it without
// the ~6% trim the canonical tight viewBox carries. Favicons, OG cards, email
// signatures still use WINGS_TIGHT_VIEWBOX so the mark keeps its breathing
// room on standalone surfaces.
export const WINGS_CROPPED_VIEWBOX = "124.497 116.237 102.174 120.823";

type SvgBase = Omit<SVGProps<SVGSVGElement>, "viewBox" | "xmlns" | "children">;

export interface WingsArgentProps extends SvgBase {
  readonly title?: string | undefined;
  readonly cropped?: boolean;
}

// Argent on Iron — the canonical mark when the canvas is already the wings'
// ground (Iron, photography behind a scrim, in-product chrome). Tight viewBox
// because the surrounding canvas does the work of giving the wings air. Pass
// `cropped` when the consumer needs the bounding box to hug the glyph exactly
// (e.g. horizontal lockups where the wordmark aligns to the wing tip).
export function WingsArgent({ title, cropped, ...rest }: WingsArgentProps) {
  return (
    <svg
      viewBox={cropped ? WINGS_CROPPED_VIEWBOX : WINGS_TIGHT_VIEWBOX}
      xmlns="http://www.w3.org/2000/svg"
      role={title ? "img" : "presentation"}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      focusable="false"
      {...rest}
    >
      <path fill="#FFFFFF" d={WINGS_PATH_D} />
    </svg>
  );
}

export interface WingsEmbossProps extends SvgBase {
  readonly title?: string | undefined;
}

// Argent on Flare via a circular ink medallion — the broadcast treatment.
// Used when the brand wants to be noticed: investor covers, billboards,
// recruiting posters, signage. Reserved for surfaces with broadcast intent.
export function WingsEmboss({ title, ...rest }: WingsEmbossProps) {
  return (
    <svg
      viewBox={WINGS_PADDED_VIEWBOX}
      xmlns="http://www.w3.org/2000/svg"
      role={title ? "img" : "presentation"}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      focusable="false"
      {...rest}
    >
      <circle cx="176" cy="177" r="126" fill="#0B0B0B" />
      <path fill="#FFFFFF" d={WINGS_PATH_D} />
    </svg>
  );
}

export interface WingsChipProps extends SvgBase {
  readonly title?: string | undefined;
}

// Argent on a rounded iron chip — the editorial / favicon / second-reference
// treatment. Carries its own ground so the wings retain Argent on Paper, on
// photography without scrim, and inside operating-system icon containers.
// Coordinates (30.01, 31.08, 291.14) are the playground's; preserved verbatim.
export function WingsChip({ title, ...rest }: WingsChipProps) {
  return (
    <svg
      viewBox={WINGS_PADDED_VIEWBOX}
      xmlns="http://www.w3.org/2000/svg"
      role={title ? "img" : "presentation"}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      focusable="false"
      {...rest}
    >
      <rect x="30.01" y="31.08" width="291.14" height="291.14" fill="#0E0E0E" rx="32" ry="32" />
      <path fill="#FFFFFF" d={WINGS_PATH_D} />
    </svg>
  );
}
