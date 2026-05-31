import type { ReactNode } from "react";
import { useAuth } from "@verself/auth-web/react";
import { getConsoleApp } from "./app-registry";
import type { ConsoleSearch } from "./console-search";
import { AccessGate } from "./access-gate";
import { ConsoleAppBoundary } from "./console-error-boundary";
import { CONSOLE_ABSOLUTE_RAIL_CLASS, CONSOLE_STAGE_HEIGHT_CLASS } from "./console-layout";
import { ConsoleTopBar } from "./console-top-bar";
import { InsightDeckShell } from "./insight-deck-shell";
import { LiveActivityLayer } from "./live-activity-layer";

export function ConsoleAppHost({
  liveActivity,
  liveActivityVisible = false,
  orgSlug,
  search,
}: {
  readonly liveActivity?: ReactNode;
  readonly liveActivityVisible?: boolean;
  readonly orgSlug: string;
  readonly search: ConsoleSearch;
}) {
  const auth = useAuth();
  const app = getConsoleApp(search.app);
  const Panel = app.Panel;
  const resetKey = `${app.id}:${search.panel ?? ""}:${search.focus ?? ""}`;
  const title = app.pageTitle ?? app.shortLabel;

  return (
    <div
      className={`absolute left-1/2 top-1/2 w-full max-w-screen-sm -translate-x-1/2 -translate-y-1/2 overflow-hidden ${CONSOLE_STAGE_HEIGHT_CLASS}`}
    >
      <header
        aria-label="Console actions"
        className={`${CONSOLE_ABSOLUTE_RAIL_CLASS} top-[calc(env(safe-area-inset-top,0px)+1rem)] z-40`}
      >
        <ConsoleTopBar orgSlug={orgSlug} title={title} />
      </header>
      {liveActivity ? (
        <LiveActivityLayer visible={liveActivityVisible}>{liveActivity}</LiveActivityLayer>
      ) : null}
      <section
        aria-label="Verself applications"
        className={`${CONSOLE_ABSOLUTE_RAIL_CLASS} bottom-[calc(env(safe-area-inset-bottom,0px)+0.625rem)] top-[calc(env(safe-area-inset-top,0px)+4.5rem)] z-10`}
      >
        <InsightDeckShell activeApp={app}>
          <ConsoleAppBoundary appLabel={app.label} resetKey={resetKey}>
            <AccessGate app={app} banner={search.banner} isSignedIn={auth.isSignedIn}>
              <Panel app={app} context={{ orgSlug, search }} />
            </AccessGate>
          </ConsoleAppBoundary>
        </InsightDeckShell>
      </section>
    </div>
  );
}
