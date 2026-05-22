import type { CSSProperties } from "react";
import type { Letter } from "~/content/letters";

export type LetterTransitionSlot = "date" | "salutation" | "body";

export type ViewTransitionStyle = CSSProperties & {
  viewTransitionName?: string;
};

export const LETTER_RETURN_TRANSITION_NAME = "letter-return";

const CONTINUATION_REVEAL_SPEED_PX_PER_SECOND = 1000;
const CONTINUATION_REVEAL_MIN_MS = 260;
const CONTINUATION_REVEAL_MAX_MS = 1200;

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n));
}

export function transitionIdent(letter: Letter, slot: LetterTransitionSlot): string {
  const safeSlug = letter.slug.replace(/[^a-zA-Z0-9_-]/g, "-");
  return `letter-${slot}-${safeSlug}`;
}

export function transitionStyle(letter: Letter, slot: LetterTransitionSlot): ViewTransitionStyle {
  return {
    viewTransitionName: transitionIdent(letter, slot),
  };
}

export function fixedTransitionStyle(name: string): ViewTransitionStyle {
  return {
    viewTransitionName: name,
  };
}

export function continuationRevealDurationMs(scrollHeight: number): number {
  const duration = (scrollHeight / CONTINUATION_REVEAL_SPEED_PX_PER_SECOND) * 1000;
  return Math.round(clamp(duration, CONTINUATION_REVEAL_MIN_MS, CONTINUATION_REVEAL_MAX_MS));
}

export function syncLetterContinuationMetrics(node: HTMLElement | null): void {
  if (!node || typeof document === "undefined") return;

  const height = Math.ceil(node.scrollHeight);
  const duration = continuationRevealDurationMs(height);
  const root = document.documentElement;
  root.style.setProperty("--letter-continuation-unfurl-duration", `${duration}ms`);
}
