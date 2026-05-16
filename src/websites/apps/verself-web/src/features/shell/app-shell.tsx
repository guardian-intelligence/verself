import { Outlet } from "@tanstack/react-router";
import { ElapsedTimeProvider } from "@verself/ui/hooks/use-elapsed-time";

// The signed-in surface has no chrome: no sidebar, no command palette, no top
// bar. Operators drive everything through the CLI/SDK/HTTP API; the browser is
// a single ambient console view. The only thing the shell still owns is the
// shared elapsed-time clock the console card reads for relative timestamps.
export function AppShell() {
  return (
    <ElapsedTimeProvider pollIntervalMs={1_000} justNowThresholdSeconds={3}>
      {/* iOS dark-mode systemBackground (pure black, #000000) behind the
          OLED widget — the signed-in surface is the only thing painted this
          way; the light console palette tokens are left untouched for
          public/policy/shadcn surfaces. color-scheme: dark so the scrollbar,
          overscroll gutter, and form chrome match. */}
      <main
        id="main"
        className="min-h-svh"
        style={{ background: "oklch(0 0 0)", colorScheme: "dark" }}
      >
        <Outlet />
      </main>
    </ElapsedTimeProvider>
  );
}
