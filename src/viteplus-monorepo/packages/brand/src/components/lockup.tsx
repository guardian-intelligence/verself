import type { CSSProperties, ReactNode } from "react";
import { WingsArgent, WingsChip, WingsEmboss } from "./wings";

export type LockupSize = "sm" | "md" | "lg";

// MARK_HEIGHT_PX is the Lockup's primary sizing number. It is the visible-ink
// height of the mark — the wings silhouette on argent, the iron tile on chip,
// the medallion disc on emboss — not the SVG bounding box. Every variant
// rescales its SVG so the visible ink lands at exactly this height, which is
// the only way to keep Workshop / Newsroom / Letters vertically consistent
// against the same wordmark. The VARIANT_BOX table below declares what each
// viewBox contains and the component computes the SVG render box from there.
const MARK_HEIGHT_PX: Record<LockupSize, number> = {
  sm: 32,
  md: 52,
  lg: 96,
};

// Lab-tuned at lg in the alignment playground: wordmark 90% of mark-h across
// the size ladder. Same ratio at every size keeps the lockup visually coherent
// whether it appears as a 32px topbar mark or a 96px hero lockup.
const WORDMARK_RATIO: Record<LockupSize, number> = {
  sm: 0.9,
  md: 0.9,
  lg: 0.9,
};

// Optical tracking per size. Tighter at display sizes, opener at small so the
// wordmark stays readable when "Guardian" is only a few tens of pixels tall.
const WORDMARK_TRACKING: Record<LockupSize, string> = {
  sm: "-0.008em",
  md: "-0.015em",
  lg: "-0.025em",
};

// Fraunces at opsz 144 — cap-height ÷ em-square. Measured once from the font
// metrics; used to derive optical gap from cap-height without measuring the
// rendered glyph at runtime. Any change to the display face needs this
// updated to match.
const FRAUNCES_144_CAP_RATIO = 0.72;

// Optical gap between mark-ink-right-edge and wordmark-ink-left-edge, as a
// fraction of wordmark cap-height. The classic logotype rule: the gap reads
// like the counter of the lead capital — close enough to lock the mark and
// word into one unit, far enough to resolve as two. 0.42 lands on the
// loose-end of that range so the mark has air to breathe against a serif
// wordmark; tighten toward 0.35 if we ever ship a sans wordmark.
const GAP_TO_CAP_RATIO = 0.42;

// Three variants, one per treatment:
//   argent  — bare cropped wings (Workshop, on Ink, and any photography/scrim
//             ground where the canvas itself carries the clearspace).
//   chip    — argent wings inside an iron rounded tile (Letters, on Paper).
//   emboss  — argent wings inside a dark circular medallion (Newsroom, on
//             Flare and Argent).
// A chip whose tile colour matches its ground is just padded bare wings; in a
// lockup the wordmark supplies the clearspace, so we never ship one.
export type LockupVariant = "argent" | "chip" | "emboss";

// Per-variant SVG geometry, declared in units of markH.
//
//   inkScale     — visible-ink height as a fraction of markH. Contained
//                  variants (chip, emboss) use markH as their container
//                  footprint: inkScale 1. Naked variants (argent) must live
//                  inside the wordmark's cap-height — a container absorbs
//                  optical mass, bare ink does not — so they downscale.
//   svgH         — rendered SVG height as a fraction of inkHeight. > 1 when
//                  the viewBox carries vertical padding past the visible ink
//                  (emboss's disc inside a square box). 1 when the visible
//                  ink fills the box.
//   svgW         — rendered SVG width as a fraction of inkHeight. Glyph-tight
//                  cropped wings are taller than they are wide, so argent
//                  renders narrower; chip/emboss are square.
//   rightBleed   — invisible horizontal padding between the visible ink's
//                  right edge and the SVG bounding box's right edge, as a
//                  fraction of inkHeight. The flex gap is measured against
//                  the SVG box edge, so we subtract this so the optical
//                  ink-to-wordmark gap stays constant across variants.
//   markNudgeY   — absolute pixels to translate the mark vertically. Line-box
//                  centering drifts above the cap-midline on wordmarks with
//                  no descenders; this is the per-variant residual after the
//                  wordmark's own translateY correction.
//
// Contained and bare variants no longer share a visible-ink vertical footprint
// at a given markH — the bare variant is smaller, because 32px of ink weighs
// more than 32px of tile-around-ink. They share *optical weight* instead,
// which is the honest thing for a lockup family to share.
const VARIANT_BOX: Record<
  LockupVariant,
  { inkScale: number; svgH: number; svgW: number; rightBleed: number; markNudgeY: number }
> = {
  // Glyph-tight cropped viewBox: 102.174 × 120.823. The visible wings are the
  // SVG box. inkScale 0.68 lands the wing ink at Fraunces cap-height with a
  // small optical overshoot, given WORDMARK_RATIO 0.9 and
  // FRAUNCES_144_CAP_RATIO 0.72 (0.9 × 0.72 ≈ 0.648; +~5% so the wing does
  // not visually sink beneath the cap line). Revisit if either ratio changes
  // or the display face is swapped.
  argent: { inkScale: 0.68, svgH: 1, svgW: 102.174 / 120.823, rightBleed: 0, markNudgeY: 0 },
  // Iron rounded rect fills the 292 viewBox to the pixel (the 291.14 × 291.14
  // rect nested in a 292 × 292 viewBox rounds to a zero bleed in practice).
  chip: { inkScale: 1, svgH: 1, svgW: 1, rightBleed: 0, markNudgeY: 0 },
  // Circular medallion: r=126 inside a 292 viewBox, leaves (292-252)/2 = 20
  // units of invisible padding on each side. We render the SVG box 292/252
  // larger than markH so the disc itself is markH across; the remaining
  // padding flows out as rightBleed. markNudgeY 1 corrects the residual
  // above-cap-midline float that line-box centering leaves behind — the
  // medallion's size amplifies any offset, so it is the variant that needs
  // the correction most.
  emboss: {
    inkScale: 1,
    svgH: 292 / 252,
    svgW: 292 / 252,
    rightBleed: (292 - 252) / 2 / 252,
    markNudgeY: 1,
  },
};

export interface LockupProps {
  readonly size?: LockupSize;
  readonly variant?: LockupVariant;
  readonly wordmark?: ReactNode;
  readonly wordmarkColor?: string;
  readonly className?: string;
  readonly style?: CSSProperties;
  readonly title?: string;
}

// Lockup — mark + wordmark, sized so the three variants share optical weight
// against the wordmark. Contained variants (chip, emboss) land their container
// at markH; bare variants (argent) land their ink at cap-height, because ink
// without a container weighs more than the same pixel count of container
// around ink. markH is the nominal unit; each variant's inkScale resolves it
// to the variant's actual visible-ink height.
//
// Both axes of the layout are typographic, not geometric:
//   • markH is the nominal unit. inkHeight = markH × inkScale is the visible
//     ink; the SVG box is derived from there.
//   • The flex gap is derived from the wordmark's cap-height minus each
//     variant's right-side invisible bleed. Changing the mark SVG container
//     (tight viewBox, padded viewBox, different padding ratio) never silently
//     widens or lengthens the lockup — the gap stays constant.
export function Lockup({
  size = "md",
  variant = "argent",
  wordmark = "Guardian",
  wordmarkColor,
  className,
  style,
  title,
}: LockupProps) {
  const markH = MARK_HEIGHT_PX[size];
  const ratio = WORDMARK_RATIO[size];
  const tracking = WORDMARK_TRACKING[size];
  const Mark = variant === "chip" ? WingsChip : variant === "emboss" ? WingsEmboss : WingsArgent;
  const box = VARIANT_BOX[variant];

  const inkHeight = markH * box.inkScale;
  const svgHeightPx = inkHeight * box.svgH;
  const svgWidthPx = inkHeight * box.svgW;
  const rightBleedPx = inkHeight * box.rightBleed;

  const wordmarkFontSize = markH * ratio;
  const capHeight = wordmarkFontSize * FRAUNCES_144_CAP_RATIO;
  const opticalGap = capHeight * GAP_TO_CAP_RATIO;
  // 6px floor keeps the pair locked at the smallest sizes where rounding
  // error and coarse-pixel rasterization would otherwise fuse the glyphs.
  const gapPx = Math.max(6, opticalGap - rightBleedPx);

  const lockupStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: `${gapPx}px`,
    padding: "8px 0",
    color: wordmarkColor,
    ...style,
  };

  const markStyle: CSSProperties = {
    width: `${svgWidthPx}px`,
    height: `${svgHeightPx}px`,
    flex: `0 0 ${svgWidthPx}px`,
    ...(box.markNudgeY ? { transform: `translateY(${box.markNudgeY}px)` } : {}),
  };

  return (
    <span className={className} style={lockupStyle} data-variant={variant}>
      {variant === "argent" ? (
        <WingsArgent title={title} cropped style={markStyle} />
      ) : (
        <Mark title={title} style={markStyle} />
      )}
      <span
        style={{
          // "Guardian" is a serif wordmark on every ground, including Workshop.
          // The treatment system flips palette, mark variant, and body type,
          // but the name of the house is the one constant — Fraunces, always.
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: `${wordmarkFontSize}px`,
          lineHeight: 1,
          letterSpacing: tracking,
          // 1px optical nudge: flex-end aligns the text-box bottom, but the
          // Fraunces baseline sits a hair above the box bottom, leaving the
          // wordmark floating a touch high against the wing tip.
          transform: "translateY(1px)",
        }}
      >
        {wordmark}
      </span>
    </span>
  );
}

export interface StackedLockupProps {
  readonly markHeight?: number;
  readonly wordmark?: ReactNode;
  readonly tagline?: ReactNode;
  readonly variant?: LockupVariant;
  readonly className?: string;
  readonly style?: CSSProperties;
}

// Stacked lockup with optional tagline ruler — the playground's section-05
// "stacked · centred · with tagline" pattern. The rule is one wing-unit wide,
// centred, derived from --wing-unit so it tracks any mark-size override.
export function StackedLockup({
  markHeight = 88,
  wordmark = "Guardian",
  tagline,
  variant = "argent",
  className,
  style,
}: StackedLockupProps) {
  const Mark = variant === "chip" ? WingsChip : variant === "emboss" ? WingsEmboss : WingsArgent;
  const wingUnit = markHeight * 0.45;
  return (
    <span
      className={className}
      style={{
        display: "inline-flex",
        flexDirection: "column",
        alignItems: "center",
        gap: "14px",
        padding: "24px 0",
        ...style,
      }}
    >
      <Mark style={{ width: `${markHeight}px`, height: `${markHeight}px` }} />
      <span
        style={{
          // See Lockup above — "Guardian" is Fraunces on every ground.
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: "28px",
          letterSpacing: "-0.01em",
          lineHeight: 1,
        }}
      >
        {wordmark}
      </span>
      {tagline && (
        <span
          style={{
            position: "relative",
            marginTop: "6px",
            paddingTop: "10px",
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "13px",
            letterSpacing: "0.2em",
            textTransform: "uppercase",
            opacity: 0.85,
          }}
        >
          <span
            aria-hidden="true"
            style={{
              position: "absolute",
              left: "50%",
              top: 0,
              width: `${wingUnit * 2}px`,
              height: "1px",
              background: "currentColor",
              opacity: 0.35,
              transform: "translateX(-50%)",
            }}
          />
          {tagline}
        </span>
      )}
    </span>
  );
}
