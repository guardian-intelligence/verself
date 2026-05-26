import type { CSSProperties } from "react";
import type { Letter } from "~/content/letters";

export type LetterTransitionSlot = "date" | "salutation" | "body";
export type LetterNavigationIntent = "open" | "return";

export type ViewTransitionStyle = CSSProperties & {
  viewTransitionName?: string;
};

export const LETTER_RETURN_TRANSITION_NAME = "letter-return";
export const LETTER_NAVIGATION_INTENT_ATTRIBUTE = "data-letter-navigation-intent";
export const LETTER_CONTINUATION_ANIMATE_ATTRIBUTE = "data-letter-continuation-animate";
export const LETTER_OPEN_VIEW_TRANSITION_TYPE = "letters-open";
export const LETTER_RETURN_VIEW_TRANSITION_TYPE = "letters-return";

const CONTINUATION_REVEAL_SPEED_PX_PER_SECOND = 1000;
const CONTINUATION_REVEAL_MIN_MS = 260;
export const CONTINUATION_REVEAL_MAX_MS = 1200;

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
