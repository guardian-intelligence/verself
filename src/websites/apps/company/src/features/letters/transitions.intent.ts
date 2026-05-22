import { createClientOnlyFn } from "@tanstack/react-start";
import type { AnchorHTMLAttributes, KeyboardEvent, MouseEvent, PointerEvent } from "react";
import {
  CONTINUATION_REVEAL_MAX_MS,
  LETTER_CONTINUATION_ANIMATE_ATTRIBUTE,
  LETTER_NAVIGATION_INTENT_ATTRIBUTE,
  LETTER_OPEN_VIEW_TRANSITION_TYPE,
  LETTER_RETURN_VIEW_TRANSITION_TYPE,
  continuationRevealDurationMs,
  type LetterNavigationIntent,
} from "~/features/letters/transitions";

// SSR routes import this file to wire Link props; createClientOnlyFn fences the
// browser work without making the whole module illegal in the server bundle.
type LetterViewTransitionOptions = {
  types: () => Array<string> | false;
};

export const LETTER_OPEN_VIEW_TRANSITION: LetterViewTransitionOptions = {
  types: () => viewTransitionTypesForIntent("open", LETTER_OPEN_VIEW_TRANSITION_TYPE) ?? false,
};

export const LETTER_RETURN_VIEW_TRANSITION: LetterViewTransitionOptions = {
  types: () => viewTransitionTypesForIntent("return", LETTER_RETURN_VIEW_TRANSITION_TYPE) ?? false,
};

const LETTER_NAVIGATION_INTENT_RESET_MS = CONTINUATION_REVEAL_MAX_MS + 1000;
const KEYBOARD_ACTIVATION_WINDOW_MS = 700;
const CONTINUATION_INTENT_REPLAY_MS = 150;

let navigationIntentReset: number | undefined;
let keyboardActivationExpiresAt = 0;
let continuationIntentReplayExpiresAt = 0;

const clearLetterNavigationIntent = createClientOnlyFn((intent?: LetterNavigationIntent): void => {
  const root = document.documentElement;
  if (!intent || root.getAttribute(LETTER_NAVIGATION_INTENT_ATTRIBUTE) === intent) {
    root.removeAttribute(LETTER_NAVIGATION_INTENT_ATTRIBUTE);
  }

  if (navigationIntentReset !== undefined) {
    window.clearTimeout(navigationIntentReset);
    navigationIntentReset = undefined;
  }
});

const markLetterNavigationIntent = createClientOnlyFn((intent: LetterNavigationIntent): void => {
  document.documentElement.setAttribute(LETTER_NAVIGATION_INTENT_ATTRIBUTE, intent);

  if (navigationIntentReset !== undefined) {
    window.clearTimeout(navigationIntentReset);
  }
  navigationIntentReset = window.setTimeout(() => {
    clearLetterNavigationIntent(intent);
  }, LETTER_NAVIGATION_INTENT_RESET_MS);
});

const consumeLetterNavigationIntent = createClientOnlyFn(
  (intent: LetterNavigationIntent): boolean => {
    const isActive =
      document.documentElement.getAttribute(LETTER_NAVIGATION_INTENT_ATTRIBUTE) === intent;
    if (isActive) {
      clearLetterNavigationIntent(intent);
    }
    return isActive;
  },
);

const consumeContinuationAnimationIntent = createClientOnlyFn((): boolean => {
  if (consumeLetterNavigationIntent("open")) {
    continuationIntentReplayExpiresAt = performance.now() + CONTINUATION_INTENT_REPLAY_MS;
    return true;
  }
  return performance.now() < continuationIntentReplayExpiresAt;
});

const viewTransitionTypesForIntent = createClientOnlyFn(
  (intent: LetterNavigationIntent, transitionType: string): Array<string> | false =>
    document.documentElement.getAttribute(LETTER_NAVIGATION_INTENT_ATTRIBUTE) === intent
      ? [transitionType]
      : false,
);

function isPlainSameTabActivation(event: MouseEvent<HTMLAnchorElement>): boolean {
  if (event.defaultPrevented) return false;
  if (event.button !== 0) return false;
  if (isRecentKeyboardActivation()) return false;
  if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return false;

  const target = event.currentTarget.getAttribute("target");
  return !target || target === "_self";
}

function markKeyboardActivation(event: KeyboardEvent<HTMLAnchorElement>): void {
  if (event.key !== "Enter" && event.key !== " ") return;
  keyboardActivationExpiresAt = performance.now() + KEYBOARD_ACTIVATION_WINDOW_MS;
  clearLetterNavigationIntent();
}

function isRecentKeyboardActivation(): boolean {
  return performance.now() < keyboardActivationExpiresAt;
}

function isPlainSameTabMouse(event: MouseEvent<HTMLAnchorElement>): boolean {
  if (event.defaultPrevented) return false;
  if (event.button > 0) return false;
  if (isRecentKeyboardActivation()) return false;
  if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return false;

  const target = event.currentTarget.getAttribute("target");
  return !target || target === "_self";
}

function isPlainSameTabPointer(event: PointerEvent<HTMLAnchorElement>): boolean {
  if (event.defaultPrevented) return false;
  if (event.button > 0) return false;
  if (isRecentKeyboardActivation()) return false;
  if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return false;

  const target = event.currentTarget.getAttribute("target");
  return !target || target === "_self";
}

export function letterNavigationIntentHandlers(
  intent: LetterNavigationIntent,
): Pick<
  AnchorHTMLAttributes<HTMLAnchorElement>,
  "onClickCapture" | "onKeyDownCapture" | "onMouseUpCapture" | "onPointerUpCapture"
> {
  return {
    // History traversal and mobile swipe-back do not dispatch activation
    // events, so route animation remains opt-in per intentional link use.
    onKeyDownCapture: markKeyboardActivation,
    onPointerUpCapture: (event) => {
      if (isPlainSameTabPointer(event)) {
        markLetterNavigationIntent(intent);
      }
    },
    onMouseUpCapture: (event) => {
      if (isPlainSameTabMouse(event)) {
        markLetterNavigationIntent(intent);
      }
    },
    onClickCapture: (event) => {
      if (isPlainSameTabActivation(event)) {
        markLetterNavigationIntent(intent);
      }
    },
  };
}

export const syncLetterContinuationMetrics = createClientOnlyFn(
  (node: HTMLElement | null): void => {
    if (!node) return;

    const height = Math.ceil(node.scrollHeight);
    const duration = continuationRevealDurationMs(height);
    const root = document.documentElement;
    root.style.setProperty("--letter-continuation-unfurl-duration", `${duration}ms`);

    const shouldAnimate =
      node.hasAttribute(LETTER_CONTINUATION_ANIMATE_ATTRIBUTE) ||
      consumeContinuationAnimationIntent();
    if (shouldAnimate) {
      node.setAttribute(LETTER_CONTINUATION_ANIMATE_ATTRIBUTE, "");
    } else {
      node.removeAttribute(LETTER_CONTINUATION_ANIMATE_ATTRIBUTE);
    }
  },
);
