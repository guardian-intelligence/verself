import type { CSSProperties, ElementType, ReactNode } from "react";

// Eyebrow — the small all-caps mono label that precedes a headline, numbers a
// specimen, or badges a status. Centralised here because the recipe is easy to
// misrender: at 11px/500/55% on Iron, Geist Mono's variable rasterizer
// quantizes vertical stems asymmetrically — "I" can render with a 2px stem
// while "T" lands on 1px, which reads as uneven stroke weight. Driving the
// `wght` axis explicitly at 600 and landing ink at `--muted` (72%) keeps the
// machine voice but gives the glyphs enough density to rasterize evenly.
//
// Three tones cover every ground:
//   • default → `--muted`  (Iron)
//   • faint   → `--muted-faint` (when paired directly with a louder eyebrow)
//   • ink     → rgba(11,11,11,0.72) for Flare and Paper grounds
// Override via `color` only when the ground is non-standard (scrim pills).

type EyebrowTone = "default" | "faint" | "ink";

const TONE_VAR: Record<EyebrowTone, string> = {
  default: "var(--muted)",
  faint: "var(--muted-faint)",
  ink: "rgba(11,11,11,0.72)",
};

export function Eyebrow({
  children,
  as,
  tone = "default",
  size = 11,
  tracking = "0.16em",
  color,
  className,
  style,
  ariaHidden,
}: {
  readonly children: ReactNode;
  readonly as?: ElementType;
  readonly tone?: EyebrowTone;
  readonly size?: number;
  readonly tracking?: string;
  readonly color?: string;
  readonly className?: string;
  readonly style?: CSSProperties;
  readonly ariaHidden?: boolean;
}) {
  const Tag = as ?? "p";
  return (
    <Tag
      aria-hidden={ariaHidden}
      className={className}
      style={{
        fontFamily: "'Geist Mono', ui-monospace, SFMono-Regular, monospace",
        fontSize: `${size}px`,
        lineHeight: 1.4,
        fontWeight: 600,
        fontVariationSettings: '"wght" 600',
        letterSpacing: tracking,
        textTransform: "uppercase",
        color: color ?? TONE_VAR[tone],
        margin: 0,
        ...style,
      }}
    >
      {children}
    </Tag>
  );
}
