import { Outlet } from "@tanstack/react-router";
import { ElapsedTimeProvider } from "@verself/ui/hooks/use-elapsed-time";
import { VerselfShell } from "~/features/landing/verself-shell";

// The signed-in surface shares the public Verself ambient shell. Operators
// drive product surfaces through the CLI/SDK/API; the browser stays a compact
// status surface with the same house chrome as the landing page.
export function AppShell() {
  return (
    <VerselfShell showNav={false}>
      <ElapsedTimeProvider pollIntervalMs={1_000} justNowThresholdSeconds={3}>
        <main id="main" className="relative min-h-svh">
          <Outlet />
        </main>
      </ElapsedTimeProvider>
    </VerselfShell>
  );
}
