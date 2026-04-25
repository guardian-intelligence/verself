import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { cn } from "@forge-metal/ui/lib/utils";
import {
  Page,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageTitle,
} from "@forge-metal/ui/components/ui/page";
import { isPathActive, SETTINGS_NAV } from "~/features/shell/nav-config";

export const Route = createFileRoute("/_shell/_authenticated/settings")({
  component: SettingsLayout,
});

// Secondary layout for the settings subtree. Single "Settings" page title,
// a vertical section list on the left, main content on the right. The
// outer Page owns rhythm; child routes render their own PageSections
// directly into the Outlet without a second PageHeader.
function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Settings</PageTitle>
          <PageDescription>Manage your profile, plan, credits, and organization.</PageDescription>
        </PageHeaderContent>
      </PageHeader>

      <div className="flex flex-col gap-8 md:flex-row md:gap-10">
        <nav aria-label="Settings sections" className="md:w-48 md:shrink-0">
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
    </Page>
  );
}
