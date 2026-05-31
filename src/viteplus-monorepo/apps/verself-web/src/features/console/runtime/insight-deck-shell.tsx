import type { ReactNode } from "react";
import type { ConsoleAppDefinition } from "./app-types";
import { CONSOLE_APP_SURFACE_RADIUS_CLASS } from "./console-layout";

export function InsightDeckShell({
  activeApp,
  children,
}: {
  readonly activeApp: ConsoleAppDefinition;
  readonly children: ReactNode;
}) {
  return (
    <div aria-label={activeApp.label} className="relative h-full min-h-[20rem] w-full">
      <article
        className={`relative z-10 h-full w-full overflow-hidden shadow-[0_30px_90px_-54px_rgb(0_0_0/0.95)] ${CONSOLE_APP_SURFACE_RADIUS_CLASS}`}
      >
        {children}
      </article>
    </div>
  );
}
