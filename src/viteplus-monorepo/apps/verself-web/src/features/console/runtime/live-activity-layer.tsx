import type { ReactNode } from "react";
import { cn } from "@verself/ui/lib/utils";
import { CONSOLE_ABSOLUTE_RAIL_CLASS, CONSOLE_LIVE_ACTIVITY_INSET_CLASS } from "./console-layout";

export function LiveActivityLayer({
  children,
  visible,
}: {
  readonly children: ReactNode;
  readonly visible: boolean;
}) {
  return (
    <section
      aria-hidden={!visible}
      aria-label="Live activity"
      className={cn(
        CONSOLE_ABSOLUTE_RAIL_CLASS,
        "pointer-events-none top-[calc(env(safe-area-inset-top,0px)+5.25rem)] z-30",
        !visible && "hidden",
      )}
    >
      <div className={`relative ${CONSOLE_LIVE_ACTIVITY_INSET_CLASS}`}>
        {children}
        <div
          aria-hidden="true"
          className="absolute left-1/2 top-full h-24 w-[78%] -translate-x-1/2 rounded-[50%] bg-black/45 blur-3xl"
        />
      </div>
    </section>
  );
}
