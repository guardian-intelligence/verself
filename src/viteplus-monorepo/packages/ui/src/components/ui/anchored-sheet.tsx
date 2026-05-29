"use client";

import * as React from "react";

import { Button } from "./button";

import { cn } from "@verself/ui/lib/utils";

const SHEET_CORNER_REFERENCE_HEIGHT_PX = 320;
const SHEET_TOP_RADIUS_MIN = 16;
const SHEET_TOP_RADIUS_MAX = 26;
const SHEET_BOTTOM_RADIUS_MIN = 22;
const SHEET_BOTTOM_RADIUS_MAX = 40;
const SHEET_ACTION_FALLBACK_PX = 88;
const SHEET_DISMISS_OFFSET_PX = 24;
const SPRING_REST_DISTANCE_PX = 0.5;
const SPRING_REST_VELOCITY = 3.2;
const DEVELOPMENT_MODE_PARAM = "developmentMode";
const DEVELOPMENT_MODE_VALUE = "1";
const ANCHORED_SHEET_PRIMARY_ACTION_SECTION_DISPLAY_NAME = "AnchoredSheetPrimaryActionSection";

// Tuned from the 60fps reference: ~2.6% overshoot after the initial launch.
const PRESENT_SPRING = {
  damping: 16.6,
  startVelocityRatio: 5,
  stiffness: 138,
};
const RESTORE_SPRING = {
  damping: 22,
  startVelocityRatio: 0,
  stiffness: 220,
};
const DISMISS_SPRING = {
  damping: 36,
  startVelocityRatio: 0,
  stiffness: 360,
};

export const ANCHORED_SHEET_PRIMARY_ACTION_BUTTON_CLASS =
  "h-[47px] min-h-[47px] w-full px-5";
const ANCHORED_SHEET_PRIMARY_ACTION_SECTION = Symbol.for(
  "verself.ui.AnchoredSheetPrimaryActionSection",
);

const useIsomorphicLayoutEffect =
  typeof window === "undefined" ? () => {} : React.useLayoutEffect;

export type SheetPhase =
  | "hidden"
  | "measuring"
  | "presenting"
  | "presented"
  | "dragging"
  | "restoring"
  | "dismissing";

export type SheetState = {
  dragY: number;
  phase: SheetPhase;
};

export type SheetEvent =
  | { type: "PRESENT" }
  | { type: "MEASURED" }
  | { type: "SPRING_SETTLED" }
  | { type: "DRAG_START" }
  | { dragY: number; type: "DRAG_MOVE" }
  | { type: "RESTORE" }
  | { dragY: number; type: "DISMISS" };

type DragSession = {
  dragY: number;
  lastTime: number;
  lastY: number;
  pointerId: number;
  startY: number;
  velocity: number;
};

type SheetCorner = {
  bottom: number;
  top: number;
};

type SpringConfig = {
  damping: number;
  startVelocityRatio?: number;
  stiffness: number;
};

type AnchoredSheetContextValue = {
  actionSectionRef: (node: HTMLDivElement | null) => void;
  phase: SheetPhase;
  sheetCorner: SheetCorner;
};

type AnchoredSheetMarkedComponent = {
  __anchoredSheetRole?: symbol;
};

export type AnchoredSheetRef = {
  hideSheet: () => void;
  dismissSheet: () => void;
  presentSheet: () => void;
};

export type AnchoredSheetProps = Omit<React.ComponentPropsWithoutRef<"section">, "children"> & {
  children: React.ReactNode;
  contentClassName?: string;
  defaultPresented?: boolean;
  onHide?: () => void;
  onSheetHidden?: () => void;
  onSheetDismissed?: () => void;
  onPresent?: () => void;
  onSheetPresented?: () => void;
};

export type AnchoredSheetPrimaryActionSectionProps = React.ComponentPropsWithoutRef<"div">;

export type AnchoredSheetPrimaryActionButtonProps = React.ComponentProps<typeof Button>;

const AnchoredSheetContext = React.createContext<AnchoredSheetContextValue | null>(null);

export function anchoredSheetReducer(state: SheetState, event: SheetEvent): SheetState {
  switch (event.type) {
    case "PRESENT":
      if (state.phase === "hidden") {
        return { dragY: 0, phase: "measuring" };
      }
      if (state.phase === "measuring") {
        return state;
      }
      return { dragY: state.dragY, phase: "presenting" };
    case "MEASURED":
      if (state.phase !== "measuring") {
        return state;
      }
      return { dragY: 0, phase: "presenting" };
    case "SPRING_SETTLED":
      if (state.phase === "presenting" || state.phase === "restoring") {
        return { dragY: 0, phase: "presented" };
      }
      if (state.phase === "dismissing") {
        return { dragY: 0, phase: "hidden" };
      }
      return state;
    case "DRAG_START":
      if (state.phase !== "presented") {
        return state;
      }
      return { dragY: 0, phase: "dragging" };
    case "DRAG_MOVE":
      if (state.phase !== "dragging") {
        return state;
      }
      return { dragY: event.dragY, phase: "dragging" };
    case "RESTORE":
      if (state.phase !== "dragging") {
        return state;
      }
      return { dragY: state.dragY, phase: "restoring" };
    case "DISMISS":
      if (state.phase === "hidden" || state.phase === "dismissing") {
        return state;
      }
      return { dragY: event.dragY, phase: "dismissing" };
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function isDevelopmentModeActive(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  const params = new URLSearchParams(window.location.search);
  return params.get(DEVELOPMENT_MODE_PARAM) === DEVELOPMENT_MODE_VALUE;
}

function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  if (target.isContentEditable) {
    return true;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === "input" || tagName === "textarea" || tagName === "select";
}

function getDismissThreshold(topSectionBaseHeight: number) {
  return Math.max(Math.min(topSectionBaseHeight * 0.34, 180), 1);
}

function getSpringConfig(phase: SheetPhase): SpringConfig {
  switch (phase) {
    case "presenting":
      return PRESENT_SPRING;
    case "restoring":
      return RESTORE_SPRING;
    case "dismissing":
      return DISMISS_SPRING;
    default:
      return RESTORE_SPRING;
  }
}

function sheetCornerRadii(height: number): SheetCorner {
  const normalized = clamp(
    (height - SHEET_CORNER_REFERENCE_HEIGHT_PX) / (SHEET_CORNER_REFERENCE_HEIGHT_PX * 2),
    0,
    1,
  );
  const top = Math.round(
    SHEET_TOP_RADIUS_MIN + (SHEET_TOP_RADIUS_MAX - SHEET_TOP_RADIUS_MIN) * normalized,
  );
  const bottom = Math.round(
    SHEET_BOTTOM_RADIUS_MIN + (SHEET_BOTTOM_RADIUS_MAX - SHEET_BOTTOM_RADIUS_MIN) * normalized,
  );
  return { bottom, top };
}

function prefersReducedMotion() {
  return (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

function assignRef<T>(ref: React.ForwardedRef<T> | undefined, value: T | null) {
  if (!ref) {
    return;
  }
  if (typeof ref === "function") {
    ref(value);
    return;
  }
  ref.current = value;
}

function composeRefs<T>(...refs: Array<React.ForwardedRef<T> | undefined>) {
  return (value: T | null) => {
    for (const ref of refs) {
      assignRef(ref, value);
    }
  };
}

function useAnchoredSheetContext(componentName: string) {
  const context = React.useContext(AnchoredSheetContext);
  if (!context) {
    throw new Error(`${componentName} must be rendered inside AnchoredSheet.`);
  }
  return context;
}

function isPrimaryActionSection(
  child: React.ReactNode,
): child is React.ReactElement<AnchoredSheetPrimaryActionSectionProps> {
  return (
    React.isValidElement(child) &&
    ((child.type as AnchoredSheetMarkedComponent).__anchoredSheetRole ===
      ANCHORED_SHEET_PRIMARY_ACTION_SECTION ||
      (typeof child.type === "function" &&
        ((child.type as React.FC).displayName ===
          ANCHORED_SHEET_PRIMARY_ACTION_SECTION_DISPLAY_NAME ||
          child.type.name === ANCHORED_SHEET_PRIMARY_ACTION_SECTION_DISPLAY_NAME)))
  );
}

function useSpringScalar({
  config,
  immediate,
  motionKey,
  initialValue,
  onRest,
  restDistance = SPRING_REST_DISTANCE_PX,
  restVelocity = SPRING_REST_VELOCITY,
  target,
}: {
  config: SpringConfig;
  immediate: boolean;
  motionKey: SheetPhase;
  initialValue?: number;
  onRest: () => void;
  restDistance?: number;
  restVelocity?: number;
  target: number;
}) {
  const [value, setValue] = React.useState(target);
  const frameRef = React.useRef<number | null>(null);
  const lastTimeRef = React.useRef<number | null>(null);
  const motionKeyRef = React.useRef<SheetPhase>(motionKey);
  const onRestRef = React.useRef(onRest);
  const valueRef = React.useRef(target);
  const velocityRef = React.useRef(0);

  useIsomorphicLayoutEffect(() => {
    onRestRef.current = onRest;
  }, [onRest]);

  useIsomorphicLayoutEffect(() => {
    if (frameRef.current !== null) {
      window.cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }

    const isNewMotion = motionKeyRef.current !== motionKey;
    motionKeyRef.current = motionKey;

    if (immediate || prefersReducedMotion()) {
      velocityRef.current = 0;
      lastTimeRef.current = null;
      valueRef.current = target;
      setValue(target);
      if (!immediate) {
        frameRef.current = window.requestAnimationFrame(() => {
          frameRef.current = null;
          onRestRef.current();
        });
      }
      return () => {
        if (frameRef.current !== null) {
          window.cancelAnimationFrame(frameRef.current);
          frameRef.current = null;
        }
      };
    }

      if (isNewMotion && typeof initialValue === "number") {
        const startVelocity =
          (target - initialValue) * (config.startVelocityRatio ?? 0);
        velocityRef.current = 0;
        lastTimeRef.current = null;
        valueRef.current = initialValue;
        setValue(initialValue);
        velocityRef.current = startVelocity;
    } else if (isNewMotion && config.startVelocityRatio !== undefined) {
      velocityRef.current = (target - valueRef.current) * config.startVelocityRatio;
    }

    const tick = (time: number) => {
      const previousTime = lastTimeRef.current ?? time;
      const dt = Math.min(Math.max((time - previousTime) / 1000, 0.001), 1 / 30);
      lastTimeRef.current = time;

      const displacement = target - valueRef.current;
      const acceleration = displacement * config.stiffness - velocityRef.current * config.damping;
      const nextVelocity = velocityRef.current + acceleration * dt;
      const nextValue = valueRef.current + nextVelocity * dt;

      velocityRef.current = nextVelocity;
      valueRef.current = nextValue;
      setValue(nextValue);

      const settled =
        Math.abs(target - nextValue) <= restDistance && Math.abs(nextVelocity) <= restVelocity;

      if (settled) {
        velocityRef.current = 0;
        valueRef.current = target;
        setValue(target);
        frameRef.current = null;
        lastTimeRef.current = null;
        onRestRef.current();
        return;
      }

      frameRef.current = window.requestAnimationFrame(tick);
    };

    if (
      Math.abs(target - valueRef.current) <= restDistance &&
      Math.abs(velocityRef.current) <= restVelocity
    ) {
      valueRef.current = target;
      setValue(target);
      frameRef.current = window.requestAnimationFrame(() => {
        frameRef.current = null;
        onRestRef.current();
      });
      return () => {
        if (frameRef.current !== null) {
          window.cancelAnimationFrame(frameRef.current);
          frameRef.current = null;
        }
      };
    }

    lastTimeRef.current = null;
    frameRef.current = window.requestAnimationFrame(tick);
    return () => {
      if (frameRef.current !== null) {
        window.cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [
    config.damping,
    config.startVelocityRatio,
    config.stiffness,
    immediate,
    motionKey,
    restDistance,
    restVelocity,
    target,
    initialValue,
  ]);

  return value;
}

const AnchoredSheetPrimaryActionSection = React.forwardRef<
  HTMLDivElement,
  AnchoredSheetPrimaryActionSectionProps
>(({ children, className, style, ...props }, ref) => {
  const context = useAnchoredSheetContext("AnchoredSheetPrimaryActionSection");

  return (
    <div
      data-slot="anchored-sheet-primary-action-section"
      ref={composeRefs(context.actionSectionRef, ref)}
      className={cn(
        "shrink-0 bg-popover px-5 pb-[max(1rem,env(safe-area-inset-bottom,0px))] pt-3",
        "relative overflow-hidden",
        "[&_[data-slot=button]]:h-[47px] [&_[data-slot=button]]:min-h-[47px] [&_[data-slot=button]]:w-full [&_[data-slot=button]]:px-5",
        "[&_[data-slot=button]]:rounded-[var(--anchored-sheet-primary-action-radius)]",
        className,
      )}
      style={{
        ...style,
        "--anchored-sheet-primary-action-radius": `${context.sheetCorner.bottom}px`,
        // Keep the action area static relative to the sheet body.
        transform: style?.transform,
      } as React.CSSProperties}
      {...props}
    >
        <div className="relative">
          <div data-slot="anchored-sheet-primary-action-hitbox" className="w-full opacity-0">
            {children}
          </div>
          <div
            data-slot="anchored-sheet-primary-action-visual"
            className="pointer-events-none absolute inset-x-0 top-0 z-20 w-full"
            aria-hidden="true"
          >
            {children}
          </div>
      </div>
    </div>
  );
});
AnchoredSheetPrimaryActionSection.displayName = "AnchoredSheetPrimaryActionSection";
(AnchoredSheetPrimaryActionSection as AnchoredSheetMarkedComponent).__anchoredSheetRole =
  ANCHORED_SHEET_PRIMARY_ACTION_SECTION;

function AnchoredSheetPrimaryActionButton({
  className,
  style,
  ...props
}: AnchoredSheetPrimaryActionButtonProps) {
  const context = useAnchoredSheetContext("AnchoredSheetPrimaryActionButton");

  return (
    <Button
      className={cn(ANCHORED_SHEET_PRIMARY_ACTION_BUTTON_CLASS, className)}
      style={{
        ...style,
        borderRadius: `${context.sheetCorner.bottom}px`,
      }}
      {...props}
    />
  );
}
AnchoredSheetPrimaryActionButton.displayName = "AnchoredSheetPrimaryActionButton";

const AnchoredSheet = React.forwardRef<AnchoredSheetRef, AnchoredSheetProps>(
  (
    {
      "aria-label": ariaLabel = "Anchored sheet",
      children,
      className,
      contentClassName,
      onHide,
      defaultPresented = false,
      onSheetHidden,
      onSheetDismissed,
      onSheetPresented,
      onPresent,
      onPointerCancel,
      onPointerDown,
      onPointerMove,
      onPointerUp,
      style,
      ...props
    },
    ref,
  ) => {
    const childArray = React.Children.toArray(children);
    const actionSections = childArray.filter(isPrimaryActionSection);
    if (actionSections.length > 1 && process.env.NODE_ENV !== "production") {
      console.warn(
        `AnchoredSheet received ${actionSections.length} primary action sections; using the last one.`,
      );
    }
    if (actionSections.length !== 1) {
      if (actionSections.length === 0) {
        throw new Error("AnchoredSheet requires at least one AnchoredSheetPrimaryActionSection child.");
      }
      if (actionSections.length > 1) {
        // Pick the deepest section when nested routes accidentally re-render duplicates.
        // This keeps the sheet visible while preserving compatibility with route-driven
        // composition during progressive refactors.
      }
    }
    const actionSection = actionSections[actionSections.length - 1] ?? actionSections[0];
    const contentChildren = childArray.filter((child) => !isPrimaryActionSection(child));

    const [state, dispatch] = React.useReducer(anchoredSheetReducer, {
      dragY: 0,
      phase: defaultPresented ? "presented" : "hidden",
    });
    const [isDevelopmentMode, setIsDevelopmentMode] = React.useState(isDevelopmentModeActive);

    const dragSessionRef = React.useRef<DragSession | null>(null);
    const panelRef = React.useRef<HTMLElement | null>(null);
    const primaryActionRef = React.useRef<HTMLDivElement | null>(null);
    const sheetTopSectionRef = React.useRef<HTMLDivElement | null>(null);
    const closeCallbacksFiredRef = React.useRef(false);
    const [sheetTargetHeight, setSheetTargetHeight] = React.useState(0);
    const [primaryActionHeight, setPrimaryActionHeight] = React.useState(SHEET_ACTION_FALLBACK_PX);
    const sheetId = React.useId();
    const sheetElementId = `anchored-sheet-${sheetId}`;
    const sheetTitleId = `${sheetElementId}-title`;
    const sheetDescriptionId = `${sheetElementId}-description`;

    const measureLayout = React.useCallback(() => {
      const topSection = sheetTopSectionRef.current;
      const action = primaryActionRef.current;
      if (!topSection || !action) {
        return;
      }
      const topSectionHeight = topSection.scrollHeight;
      const actionHeight = action.getBoundingClientRect().height;

      const nextHeight = Math.max(Math.ceil(topSectionHeight + actionHeight), 0);
      setSheetTargetHeight((previousHeight) => {
        return Math.abs(previousHeight - nextHeight) > 0.5 ? nextHeight : previousHeight;
      });

      if (actionHeight > 1) {
        setPrimaryActionHeight((previousHeight) => {
          return Math.abs(previousHeight - actionHeight) > 0.5 ? actionHeight : previousHeight;
        });
      }
    }, []);

    const setActionSectionNode = React.useCallback(
      (node: HTMLDivElement | null) => {
        primaryActionRef.current = node;
        if (node) {
          measureLayout();
        }
      },
      [measureLayout],
    );

    const dismissThreshold = getDismissThreshold(Math.max(sheetTargetHeight - primaryActionHeight, 0));
    const dismissOffsetY = React.useMemo(
      () => sheetTargetHeight + SHEET_DISMISS_OFFSET_PX,
      [sheetTargetHeight],
    );
    const sheetTranslateYTarget = React.useMemo(() => {
      switch (state.phase) {
        case "presenting":
        case "presented":
        case "restoring":
          return 0;
        case "dragging":
          return state.dragY;
        case "dismissing":
          return dismissOffsetY;
        case "hidden":
        case "measuring":
          return dismissOffsetY;
      }
    }, [dismissOffsetY, state.dragY, state.phase]);

    const handleSpringRest = React.useCallback(() => {
      if (state.phase === "presenting") {
        dispatch({ type: "SPRING_SETTLED" });
        onSheetPresented?.();
        return;
      }
      if (state.phase === "restoring") {
        dispatch({ type: "SPRING_SETTLED" });
        return;
      }
      if (state.phase === "dismissing") {
        if (!closeCallbacksFiredRef.current) {
          closeCallbacksFiredRef.current = true;
          dispatch({ type: "SPRING_SETTLED" });
          onSheetHidden?.();
          onSheetDismissed?.();
        }
      }
    }, [onSheetDismissed, onSheetHidden, onSheetPresented, state.phase]);

    const sheetTranslateY = useSpringScalar({
      config: getSpringConfig(state.phase),
      immediate: state.phase === "measuring" || state.phase === "dragging",
      motionKey: state.phase,
      onRest: handleSpringRest,
      target: sheetTranslateYTarget,
    });

    const panelHeight = Math.max(sheetTargetHeight, 0);
    const sheetCorner = React.useMemo(
      () => sheetCornerRadii(Math.max(panelHeight, SHEET_CORNER_REFERENCE_HEIGHT_PX)),
      [panelHeight],
    );
    const contextValue = React.useMemo<AnchoredSheetContextValue>(
      () => ({
        actionSectionRef: setActionSectionNode,
        phase: state.phase,
        sheetCorner,
      }),
      [
        setActionSectionNode,
        sheetCorner,
        state.phase,
      ],
    );

    const presentSheet = React.useCallback(() => {
      if (state.phase === "presenting") {
        return;
      }
      if (state.phase === "dismissing") {
        closeCallbacksFiredRef.current = false;
      }
      dragSessionRef.current = null;
      closeCallbacksFiredRef.current = false;
      dispatch({ type: "PRESENT" });
    }, [state.phase]);

    const hideSheet = React.useCallback(() => {
      if (state.phase === "hidden" || state.phase === "dismissing") {
        return;
      }
      dragSessionRef.current = null;
      dispatch({ dragY: state.dragY, type: "DISMISS" });
    }, [state.dragY, state.phase]);

    const dismissSheet = React.useCallback(() => {
      hideSheet();
    }, [hideSheet]);

    const requestPresent = React.useCallback(() => {
      if (onPresent) {
        onPresent();
        return;
      }
      presentSheet();
    }, [onPresent, presentSheet]);

    const requestHide = React.useCallback(() => {
      if (onHide) {
        onHide();
        return;
      }
      hideSheet();
    }, [hideSheet, onHide]);

    const closeOnEscape = React.useCallback(
      (event: KeyboardEvent) => {
        if (event.key !== "Escape") {
          return;
        }

        if (state.phase === "measuring") {
          return;
        }

        requestHide();
      },
      [requestHide, state.phase],
    );

    React.useImperativeHandle(
      ref,
      () => ({
        hideSheet,
        dismissSheet,
        presentSheet,
      }),
      [hideSheet, dismissSheet, presentSheet],
    );

    const refreshDevelopmentMode = React.useCallback(() => {
      setIsDevelopmentMode(isDevelopmentModeActive());
    }, []);

    React.useEffect(() => {
      refreshDevelopmentMode();
      if (typeof window === "undefined") {
        return;
      }

      window.addEventListener("popstate", refreshDevelopmentMode);
      return () => {
        window.removeEventListener("popstate", refreshDevelopmentMode);
      };
    }, [refreshDevelopmentMode]);

    const toggleSheet = React.useCallback(() => {
      if (state.phase === "hidden" || state.phase === "dismissing") {
        requestPresent();
        return;
      }
      requestHide();
    }, [requestHide, requestPresent, state.phase]);

    const toggleSheetOnKey = React.useCallback(
      (event: KeyboardEvent) => {
        if (!isDevelopmentMode) {
          return;
        }

        const key = event.key.toLowerCase();
        if (key !== "a" || isEditableElement(event.target)) {
          return;
        }

        if (event.repeat) {
          return;
        }

        event.preventDefault();
        toggleSheet();
      },
      [isDevelopmentMode, toggleSheet],
    );

    useIsomorphicLayoutEffect(() => {
      if (!isDevelopmentMode) {
        return;
      }

      if (typeof window === "undefined") {
        return;
      }

      window.addEventListener("keydown", toggleSheetOnKey);
      return () => {
        window.removeEventListener("keydown", toggleSheetOnKey);
      };
    }, [isDevelopmentMode, toggleSheetOnKey]);

    const beginDrag = React.useCallback(
      (event: React.PointerEvent<HTMLElement>) => {
        if (state.phase !== "presented") {
          return;
        }
        event.currentTarget.setPointerCapture(event.pointerId);
        dragSessionRef.current = {
          dragY: 0,
          lastTime: event.timeStamp,
          lastY: event.clientY,
          pointerId: event.pointerId,
          startY: event.clientY,
          velocity: 0,
        };
        dispatch({ type: "DRAG_START" });
      },
      [state.phase],
    );

    const updateDrag = React.useCallback(
      (event: React.PointerEvent<HTMLElement>) => {
        const session = dragSessionRef.current;
        if (!session || session.pointerId !== event.pointerId) {
          return;
        }

        const elapsed = Math.max(event.timeStamp - session.lastTime, 1);
        const rawDragY = Math.max(event.clientY - session.startY, 0);
        const clampedDragY = Math.min(rawDragY, dismissOffsetY);
        session.velocity = (event.clientY - session.lastY) / elapsed;
        session.lastTime = event.timeStamp;
        session.lastY = event.clientY;
        session.dragY = clampedDragY;
        dispatch({ dragY: clampedDragY, type: "DRAG_MOVE" });
      },
      [dismissOffsetY],
    );

    const completeDrag = React.useCallback(() => {
      const session = dragSessionRef.current;
      if (!session) {
        return;
      }
      const shouldDismiss = session.dragY > dismissThreshold || session.velocity > 0.65;
      dragSessionRef.current = null;
      if (shouldDismiss) {
        dispatch({ dragY: session.dragY, type: "DISMISS" });
        return;
      }
      dispatch({ type: "RESTORE" });
    }, [dismissThreshold]);

    const finishDrag = React.useCallback(
      (event: React.PointerEvent<HTMLElement>) => {
        const session = dragSessionRef.current;
        if (!session || session.pointerId !== event.pointerId) {
          return;
        }

        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          event.currentTarget.releasePointerCapture(event.pointerId);
        }
        completeDrag();
      },
      [completeDrag],
    );

    const cancelDrag = React.useCallback((event: React.PointerEvent<HTMLElement>) => {
      const session = dragSessionRef.current;
      if (!session || session.pointerId !== event.pointerId) {
        return;
      }
      dragSessionRef.current = null;
      dispatch({ type: "RESTORE" });
    }, []);

    const finishCapturedDrag = React.useCallback(
      (event: React.PointerEvent<HTMLElement>) => {
        const session = dragSessionRef.current;
        if (!session || session.pointerId !== event.pointerId) {
          return;
        }
        completeDrag();
      },
      [completeDrag],
    );

    useIsomorphicLayoutEffect(() => {
      if (state.phase !== "measuring") {
        return;
      }
      measureLayout();
      const frame = window.requestAnimationFrame(() => {
        dispatch({ type: "MEASURED" });
      });
      return () => {
        window.cancelAnimationFrame(frame);
      };
    }, [measureLayout, state.phase]);

    useIsomorphicLayoutEffect(() => {
      measureLayout();
      if (typeof ResizeObserver === "undefined") {
        return;
      }

      const topSection = sheetTopSectionRef.current;
      const action = primaryActionRef.current;
      if (!topSection || !action) {
        return;
      }

      const observer = new ResizeObserver(() => {
        measureLayout();
      });
      observer.observe(action);
      observer.observe(topSection);
      return () => {
        observer.disconnect();
      };
    }, [measureLayout, state.phase]);

    useIsomorphicLayoutEffect(() => {
      if (typeof window === "undefined") {
        return;
      }

      window.addEventListener("resize", measureLayout);
      return () => {
        window.removeEventListener("resize", measureLayout);
      };
    }, [measureLayout]);

    useIsomorphicLayoutEffect(() => {
      if (state.phase === "hidden" || state.phase === "measuring") {
        return;
      }

      window.addEventListener("keydown", closeOnEscape);
      return () => {
        window.removeEventListener("keydown", closeOnEscape);
      };
    }, [closeOnEscape, state.phase]);

    const shouldRender = isDevelopmentMode || state.phase !== "hidden";
    if (!shouldRender) {
      return null;
    }

    return (
      <AnchoredSheetContext.Provider value={contextValue}>
        <section
          id={sheetElementId}
          aria-labelledby={sheetTitleId}
          aria-describedby={sheetDescriptionId}
          aria-label={ariaLabel}
          aria-modal={
            state.phase === "measuring" || state.phase === "hidden" ? "false" : "true"
          }
          data-slot="anchored-sheet"
          data-state={state.phase}
          ref={panelRef}
          role="dialog"
          tabIndex={-1}
          aria-hidden={state.phase === "hidden" ? "true" : "false"}
          className={cn(
            "fixed bottom-[max(1rem,env(safe-area-inset-bottom,0px))] inset-x-0 z-50 mx-auto flex w-[calc(100vw_-_2rem)] max-w-[28rem] flex-col overflow-hidden bg-popover bg-clip-padding text-popover-foreground shadow-[0_-1px_2px_rgb(0_0_0/0.04),0_-20px_56px_-32px_rgb(0_0_0/0.28)] ring-1 ring-foreground/5 will-change-[transform] select-none",
            className,
          )}
          style={{
            ...style,
            borderTopLeftRadius: `${sheetCorner.top}px`,
            borderTopRightRadius: `${sheetCorner.top}px`,
            borderBottomLeftRadius: `${sheetCorner.bottom}px`,
            borderBottomRightRadius: `${sheetCorner.bottom}px`,
            height: `${panelHeight}px`,
            transform: `translate3d(0, ${sheetTranslateY}px, 0)`,
            visibility:
              state.phase === "measuring" || state.phase === "hidden" ? "hidden" : style?.visibility,
            pointerEvents: state.phase === "hidden" ? "none" : style?.pointerEvents,
          }}
          onPointerCancel={(event) => {
            onPointerCancel?.(event);
          }}
          onPointerDown={(event) => {
            onPointerDown?.(event);
          }}
          onPointerMove={(event) => {
            onPointerMove?.(event);
          }}
          onPointerUp={(event) => {
            onPointerUp?.(event);
          }}
          {...props}
        >
          <h2 id={sheetTitleId} className="sr-only">
            {ariaLabel}
          </h2>
          <p id={sheetDescriptionId} className="sr-only">
            Sheet content with sign-in options and a close action.
          </p>
          <div
            className="relative min-h-0 overflow-hidden"
          >
            <div
              className="absolute inset-x-0 top-0 z-10 flex h-12 cursor-grab touch-none items-start justify-center pt-3.5"
              onPointerDown={beginDrag}
              onPointerCancel={cancelDrag}
              onLostPointerCapture={finishCapturedDrag}
              onPointerMove={updateDrag}
              onPointerUp={finishDrag}
            >
              <span aria-hidden="true" className="pointer-events-none h-1 w-9 rounded-full bg-foreground/12" />
              <a
                href="/"
                aria-controls={sheetElementId}
                aria-expanded={state.phase !== "measuring"}
                aria-label="Close"
                className="pointer-events-auto absolute right-3 top-2 border border-transparent px-3 py-2 text-sm font-medium leading-none transition-all duration-150 ease-[cubic-bezier(0.16,1,0.3,1)] hover:border-foreground/14 hover:bg-foreground/10 hover:text-foreground focus-visible:bg-foreground/12 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30"
                style={{
                  borderRadius: `${sheetCorner.top}px`,
                }}
                role="button"
                onPointerDown={(event) => {
                  event.stopPropagation();
                  requestHide();
                }}
                onPointerCancel={(event) => {
                  cancelDrag(event);
                }}
                onClick={(event) => {
                  event.preventDefault();
                  requestHide();
                }}
              >
                Close
              </a>
            </div>
            <div
              data-slot="anchored-sheet-content"
              ref={sheetTopSectionRef}
              className={cn("relative flex min-h-0 flex-col overflow-y-auto px-5 pb-4 pt-12", contentClassName)}
            >
              {contentChildren}
            </div>
          </div>
          {actionSection}
          </section>
      </AnchoredSheetContext.Provider>
    );
  },
);
AnchoredSheet.displayName = "AnchoredSheet";

export {
  AnchoredSheet,
  AnchoredSheetPrimaryActionButton,
  AnchoredSheetPrimaryActionSection,
};
