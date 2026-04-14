import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { cn } from "@forge-metal/ui/lib/utils";
import { isPathActive, SETTINGS_NAV } from "~/features/shell/nav-config";

export const Route = createFileRoute("/_authenticated/settings")({
  component: SettingsLayout,
});

// Secondary layout for the settings subtree. Claude.ai-style: single
// "Settings" page title at the top, a vertical section list on the left,
// and main content on the right. On mobile the section list collapses
// into a horizontal scroll strip above the content.
function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">
          Manage your plan, credits, and organization.
        </p>
      </header>

      <div className="flex flex-col gap-6 md:flex-row md:gap-10">
        <nav
          aria-label="Settings sections"
          className="md:w-48 md:shrink-0"
        >
          <ul className="flex gap-1 overflow-x-auto md:flex-col">
            {SETTINGS_NAV.map((entry) => {
              const active = isPathActive(path, entry);
              return (
                <li key={entry.id}>
                  <Link
                    to={entry.to}
                    data-testid={`settings-tab-${entry.id}`}
                    data-status={active ? "active" : "inactive"}
                    className={cn(
                      "block whitespace-nowrap rounded-md px-3 py-2 text-sm",
                      active
                        ? "bg-accent font-medium text-accent-foreground"
                        : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                    )}
                  >
                    {entry.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  );
}
