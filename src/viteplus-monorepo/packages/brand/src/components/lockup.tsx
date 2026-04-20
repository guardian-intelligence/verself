import type { CSSProperties, ReactNode } from "react";
import { WingsArgent, WingsChip, WingsEmboss } from "./wings";

export type LockupSize = "sm" | "md" | "lg";

const MARK_HEIGHT_PX: Record<LockupSize, number> = {
  sm: 32,
  md: 52,
  lg: 96,
};

const WORDMARK_RATIO: Record<LockupSize, number> = {
  // Letter-spacing & ratio match the playground's per-size tuning so the
  // small lockup retains optical pairing and the large one doesn't go airy.
  sm: 0.75,
  md: 0.72,
  lg: 0.78,
};

const WORDMARK_TRACKING: Record<LockupSize, string> = {
  sm: "-0.008em",
  md: "-0.015em",
  lg: "-0.025em",
};

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

// Lockup — mark + wordmark, sharing the playground's single sizing rule.
// Internal gap clamps to clamp(8px, 0.28·mark-h, 18px) so small surfaces
// still pair, large ones don't fly apart, and middle sizes follow geometry.
export function Lockup({
  size = "md",
  variant = "argent",
  wordmark = "Guardian Intelligence",
  wordmarkColor,
  className,
  style,
  title,
}: LockupProps) {
  const markH = MARK_HEIGHT_PX[size];
  const ratio = WORDMARK_RATIO[size];
  const tracking = WORDMARK_TRACKING[size];
  const Mark = variant === "chip" ? WingsChip : variant === "emboss" ? WingsEmboss : WingsArgent;

  const lockupStyle: CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    gap: `clamp(8px, calc(${markH}px * 0.28), 18px)`,
    padding: "8px 0",
    color: wordmarkColor,
    ...style,
  };

  return (
    <span className={className} style={lockupStyle}>
      <Mark
        title={title}
        style={{ width: `${markH}px`, height: `${markH}px`, flex: `0 0 ${markH}px` }}
      />
      <span
        style={{
          fontFamily: "'Fraunces', Georgia, serif",
          fontVariationSettings: '"opsz" 144, "SOFT" 30',
          fontWeight: 400,
          fontSize: `${markH * ratio}px`,
          lineHeight: 1,
          letterSpacing: tracking,
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
  wordmark = "Guardian Intelligence",
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
