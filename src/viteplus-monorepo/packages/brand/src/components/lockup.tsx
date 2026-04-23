import type { CSSProperties, ReactNode } from "react";
import { WingsArgent, WingsChip, WingsEmboss } from "./wings";

export type LockupSize = "sm" | "md" | "lg";

// MARK_HEIGHT_PX is the Lockup's primary sizing number. With the canonical
// tight viewBox (140×140), mark-h is both width and height. With the cropped
// viewBox (≈102×121) used by the Argent variant below, mark-h sets the SVG
// width; the height follows the glyph aspect ratio (≈ mark-h × 1.182).
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

// Three variants, one per treatment:
//   argent  — bare cropped wings (Workshop, on Ink, and any photography/scrim
//             ground where the canvas itself carries the clearspace).
//   chip    — argent wings inside an iron rounded tile (Letters, on Paper).
//   emboss  — argent wings inside a dark circular medallion (Newsroom, on
//             Flare and Argent).
// A chip whose tile colour matches its ground is just padded bare wings; in a
// lockup the wordmark supplies the clearspace, so we never ship one.
export type LockupVariant = "argent" | "chip" | "emboss";

export interface LockupProps {
  readonly size?: LockupSize;
  readonly variant?: LockupVariant;
  readonly wordmark?: ReactNode;
  readonly wordmarkColor?: string;
  readonly className?: string;
  readonly style?: CSSProperties;
  readonly title?: string;
}

// Lockup — mark + wordmark, tuned in the alignment playground so the wordmark
// bottom-aligns with the lower-wing tip. Argent uses the glyph-cropped viewBox
// so the SVG bounding box equals the visible ink; chip/emboss keep their
// padded viewBox because the tile/medallion is itself visible on its ground
// and IS the edge the eye reads. Gap is clamp(8px, 0.28·mark-h, 18px) —
// proportional to the visible mark, with an 8px floor so sm lockups still
// read as a pair and an 18px ceiling so lg doesn't fly apart. This formula
// only produces consistent optical spacing because every shipped variant has
// its visible ink filling the SVG box; don't introduce a variant that pads
// the bounding box with invisible-on-ground fill or the gap drifts larger.
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
  const useCropped = variant === "argent";
  const Mark = variant === "chip" ? WingsChip : variant === "emboss" ? WingsEmboss : WingsArgent;

  const lockupStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: `clamp(8px, calc(${markH}px * 0.28), 18px)`,
    padding: "8px 0",
    color: wordmarkColor,
    ...style,
  };

  // When cropped, the SVG is wider-than-square (markH × ~markH·1.18) and the
  // height must be auto so the aspect ratio holds.
  const markStyle: CSSProperties = useCropped
    ? { width: `${markH}px`, height: "auto", flex: `0 0 ${markH}px` }
    : { width: `${markH}px`, height: `${markH}px`, flex: `0 0 ${markH}px` };

  return (
    <span className={className} style={lockupStyle} data-variant={variant}>
      {useCropped ? (
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
          fontSize: `${markH * ratio}px`,
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
