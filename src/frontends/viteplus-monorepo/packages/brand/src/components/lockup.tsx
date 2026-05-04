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
//
// sm (22) is the masthead / chrome / bookplate size. It is deliberately
// quiet — the house name whispers at the top of the page so the content can
// speak. The Paris Review nameplate, The Baffler's masthead, the NYT Opinion
// bug: all of them size the volume identifier smaller than you expect on
// first draft. This lockup follows that tradition. md/lg are hero-scale
// specimens (photography, business cards, /design ladders); they do not
// ship in live chrome.
const MARK_HEIGHT_PX: Record<LockupSize, number> = {
  sm: 22,
  md: 52,
  lg: 96,
};

// Wordmark ratio — wordmark font-size as a fraction of markH. At masthead
// scale (sm) the wordmark runs much smaller than markH so the mark, not the
// text, carries the visual mass — classic bookplate proportions where the
// caps read as a label beside the device. md/lg stay near-1:1 so a hero
// lockup can anchor a photograph or a cover.
const WORDMARK_RATIO: Record<LockupSize, number> = {
  sm: 0.5,
  md: 0.95,
  lg: 0.95,
};

// Weight per size. Tracked uppercase caps thin out as they shrink; bumping
// to 600 at masthead scale keeps the strokes legible against the mark's
// black ink without making GUARDIAN louder than it should be. At hero
// scales the strokes already carry weight, so 500 is plenty.
const WORDMARK_WEIGHT: Record<LockupSize, number> = {
  sm: 600,
  md: 500,
  lg: 500,
};

// Optical tracking per size. Uppercase logotypes breathe at display sizes and
// need a touch more tracking at small sizes so GUARDIAN does not collide with
// itself when rendered at favicon-adjacent resolutions. These are per-size
// positive values — the exact opposite of the old Fraunces mixed-case curve,
// which tightened with size.
const WORDMARK_TRACKING: Record<LockupSize, string> = {
  sm: "0.26em",
  md: "0.14em",
  lg: "0.12em",
};

// Geist Variable — cap-height ÷ em-square. Measured once from the font
// metrics; used to derive the optical gap from cap-height without measuring
// the rendered glyph at runtime. Any change to the wordmark face needs this
// updated to match. (The prior face was Fraunces at opsz 144 with a cap ratio
// of 0.72 — Geist is a hair shorter, which matters when the gap math is
// cap-height-relative.)
const WORDMARK_CAP_RATIO = 0.7;

// Cap-overshoot ratio — the fraction of font-size by which the visible cap
// glyphs sit above the line-box vertical centerline. Geist's typo metrics
// place the baseline below the line-box midline, so even with line-height
// tight to cap-height the caps render high. Measured at sm (11px font-size,
// 0.7 cap-ratio): cap mid landed 0.836px above line-box mid, giving
// 0.836 / 11 ≈ 0.076. Used to translateY the wordmark down so its visible
// cap centroid coincides with the SVG center (= chip / disc center) under
// flex align-items: center. Per-face — re-measure when swapping wordmark.
const CAP_OVERSHOOT_RATIO = 0.076;

// Optical gap between mark-ink-right-edge and wordmark-ink-left-edge, as a
// fraction of wordmark cap-height. The classic logotype rule: the gap reads
// like the counter of the lead capital — close enough to lock the mark and
// word into one unit, far enough to resolve as two. 0.35 lands on the tighter
// end of that range because a sans wordmark holds its ink closer to the mark
// than a serif, and the uppercase caps do not need the extra air that
// serif-with-descenders demanded. (The prior constant was 0.42 for Fraunces;
// the lockup.tsx file you are reading predicted this tightening explicitly.)
const GAP_TO_CAP_RATIO = 0.35;

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
//
// Contained and bare variants no longer share a visible-ink vertical footprint
// at a given markH — the bare variant is smaller, because 32px of ink weighs
// more than 32px of tile-around-ink. They share *optical weight* instead,
// which is the honest thing for a lockup family to share.
const VARIANT_BOX: Record<
  LockupVariant,
  { inkScale: number; svgH: number; svgW: number; rightBleed: number }
> = {
  // Padded viewBox (292 × 292) with no surrounding tile — the same internal
  // geometry as chip and emboss but with the frame omitted. The wings land
  // at the exact same pixel position and size they have inside the chip
  // (102.174/292 wide × 120.823/292 tall of markH); the surrounding 22×22
  // box is transparent. Sharing the 292-unit footprint with the framed
  // variants is what keeps the wordmark x-position stable across
  // Workshop / Letters / Newsroom — without it, the bare-wing SVG is
  // ~7.7px wide while chip/emboss are 22px, and the wordmark jumps
  // 14px on every cross-section transition.
  argent: { inkScale: 1, svgH: 1, svgW: 1, rightBleed: 0 },
  // Iron rounded rect fills the 292 viewBox to the pixel (the 291.14 × 291.14
  // rect nested in a 292 × 292 viewBox rounds to a zero bleed in practice).
  chip: { inkScale: 1, svgH: 1, svgW: 1, rightBleed: 0 },
  // Circular medallion: r=126 inside a 292 viewBox, leaves (292-252)/2 = 20
  // units of invisible padding on each side. We render the SVG box 292/252
  // larger than markH so the disc itself is markH across; the remaining
  // padding flows out as rightBleed.
  emboss: {
    inkScale: 1,
    svgH: 292 / 252,
    svgW: 292 / 252,
    rightBleed: (292 - 252) / 2 / 252,
  },
};

export interface LockupProps {
  readonly size?: LockupSize;
  readonly variant?: LockupVariant;
  readonly wordmark?: ReactNode;
  readonly wordmarkColor?: string;
  // Optional section suffix. When present, renders as ` · {section}` in the
  // same face/weight/tracking as the wordmark, so GUARDIAN · LETTERS reads as
  // one tracked logotype, not a wordmark plus a tagline. Section is always
  // uppercased by CSS — pass "Letters" or "LETTERS" and get the same result.
  // Omit on the house root (`/`); supply on every section surface.
  readonly section?: string;
  readonly className?: string;
  readonly style?: CSSProperties;
  readonly title?: string;
}

// Lockup — mark + uppercase-tracked wordmark, sized so the three variants
// share optical weight against the wordmark. Contained variants (chip,
// emboss) land their container at markH; bare variants (argent) land their
// ink at cap-height, because ink without a container weighs more than the
// same pixel count of container around ink. markH is the nominal unit; each
// variant's inkScale resolves it to the variant's actual visible-ink height.
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
  section,
  className,
  style,
  title,
}: LockupProps) {
  const markH = MARK_HEIGHT_PX[size];
  const ratio = WORDMARK_RATIO[size];
  const weight = WORDMARK_WEIGHT[size];
  const tracking = WORDMARK_TRACKING[size];
  const box = VARIANT_BOX[variant];

  const inkHeight = markH * box.inkScale;
  const svgHeightPx = inkHeight * box.svgH;
  const svgWidthPx = inkHeight * box.svgW;
  const rightBleedPx = inkHeight * box.rightBleed;

  const wordmarkFontSize = markH * ratio;
  const capHeight = wordmarkFontSize * WORDMARK_CAP_RATIO;
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
  };

  // Uppercase sans with no descenders sits in the upper half of the em-box;
  // flex-centering the em-box drops the caps visually low against a chip or
  // medallion that is centered by its geometric box. line-height tight to
  // cap-height collapses the em-box onto the caps so flex-align-items: center
  // lands the line-box center on the chip center.
  //
  // The remaining cap-centroid drift comes from Geist's font metrics: the
  // baseline does not sit at the line-box vertical centerline, so the actual
  // cap GLYPHS render slightly above the line-box midline. Empirical
  // measurement at 11px shows ~7.6% of font-size of cap-overshoot (caps
  // 0.836px above their bounding box mid). A translateY of that amount
  // pushes the visible cap centroid onto the chip's geometric center.
  // CAP_OVERSHOOT_RATIO is per-face — Geist Variable; rebaselining the
  // wordmark face (e.g. swapping in Inter) needs this re-measured.
  //
  // Geist is hard-coded at the top of the stack (rather than read through
  // var(--font-sans)) so the lockup renders the Guardian wordmark the same
  // way whether the consumer's host registers the Tailwind @theme token or
  // not — the brand package is the authority on the wordmark face, not the
  // host app's CSS token graph.
  const wordmarkStyle: CSSProperties = {
    fontFamily: "'Geist', ui-sans-serif, system-ui, sans-serif",
    fontWeight: weight,
    fontSize: `${wordmarkFontSize}px`,
    lineHeight: WORDMARK_CAP_RATIO,
    letterSpacing: tracking,
    textTransform: "uppercase",
    whiteSpace: "nowrap",
    transform: `translateY(${wordmarkFontSize * CAP_OVERSHOOT_RATIO}px)`,
  };

  return (
    <span className={className} style={lockupStyle} data-variant={variant} data-lockup="">
      {variant === "argent" ? (
        // Figure-ground compensation: bare wings on the canvas read smaller
        // than the same pixel footprint inside chip/emboss because a framed
        // mark borrows visual weight from its container. Scale up only at
        // chrome size (sm) where the eye reads the mark at small pixel
        // counts; md/lg specimens are large enough that the optical illusion
        // does not kick in. 1.21 lands two compounded +10% iterations — the
        // value tuned by eye against the chip on Letters.
        <WingsArgent
          title={title}
          viewBoxMode="padded"
          style={markStyle}
          wingsScale={size === "sm" ? 1.21 : 1}
        />
      ) : variant === "chip" ? (
        <WingsChip title={title} style={markStyle} />
      ) : (
        // At chrome size (sm) the wings read a touch heavy inside the
        // medallion — anti-aliased ink concentrates visual mass at 22px in
        // a way that doesn't show at md/lg. Scale the inner mark down only
        // at sm so the disc itself stays the same size against the wordmark.
        <WingsEmboss title={title} style={markStyle} wingsScale={size === "sm" ? 0.92 : 1} />
      )}
      <span style={wordmarkStyle} data-lockup-wordmark="">
        {wordmark}
        {section ? (
          <>
            <span aria-hidden="true"> · </span>
            {section}
          </>
        ) : null}
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
// centred, derived from --wing-unit so it tracks any mark-size override. The
// wordmark itself sets in the same Geist uppercase dress as the horizontal
// Lockup; the tagline sits a rung below at a smaller size and wider tracking.
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
        gap: "18px",
        padding: "24px 0",
        ...style,
      }}
    >
      <Mark style={{ width: `${markHeight}px`, height: `${markHeight}px` }} />
      <span
        style={{
          fontFamily: "'Geist', ui-sans-serif, system-ui, sans-serif",
          fontWeight: 500,
          fontSize: "24px",
          letterSpacing: "0.16em",
          lineHeight: 1,
          textTransform: "uppercase",
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
            fontFamily: "'Geist', ui-sans-serif, system-ui, sans-serif",
            fontWeight: 500,
            fontSize: "12px",
            letterSpacing: "0.26em",
            textTransform: "uppercase",
            opacity: 0.75,
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
