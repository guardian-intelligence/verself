import { Outlet } from "@tanstack/react-router";
import { ElapsedTimeProvider } from "@verself/ui/hooks/use-elapsed-time";

// The signed-in surface has no chrome: no sidebar, no command palette, no top
// bar. Operators drive everything through the CLI/SDK/HTTP API; the browser is
// a single ambient console view. The only thing the shell still owns is the
// shared elapsed-time clock the console card reads for relative timestamps.
export function AppShell() {
  return (
    <ElapsedTimeProvider pollIntervalMs={1_000} justNowThresholdSeconds={3}>
      <main id="main" className="min-h-svh">
        <Outlet />
      </main>
    </ElapsedTimeProvider>
  );
}
