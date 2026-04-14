import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { cn } from "@forge-metal/ui/lib/utils";
import { isPathActive, SETTINGS_NAV } from "~/features/shell/nav-config";

export const Route = createFileRoute("/_authenticated/settings")({
  component: SettingsLayout,
});

// Secondary layout shared by every settings subpage. Acts as the "drawer"
// inside the main app shell: horizontal tabs on desktop, a flat stack on
// mobile. Follows Linear / Vercel / GitHub settings conventions: one tab
// strip + one outlet. No Radix overlays, no dropdowns.
function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-xs uppercase tracking-[0.2em] text-muted-foreground">
          Settings
        </p>
        <h1 className="text-2xl font-semibold">Account &amp; organization</h1>
      </header>

      <nav
        aria-label="Settings sections"
        className="flex flex-wrap gap-4 border-b border-foreground"
      >
        {SETTINGS_NAV.map((entry) => {
          const active = isPathActive(path, entry);
          return (
            <Link
              key={entry.id}
              to={entry.to}
              data-testid={`settings-tab-${entry.id}`}
              data-status={active ? "active" : "inactive"}
              className={cn(
                "-mb-px border-b-2 px-1 pb-2 font-mono text-xs uppercase tracking-[0.15em]",
                active
                  ? "border-foreground text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {entry.label}
            </Link>
          );
        })}
      </nav>

      <Outlet />
    </div>
  );
}
