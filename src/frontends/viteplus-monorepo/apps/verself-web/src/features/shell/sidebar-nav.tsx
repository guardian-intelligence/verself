import { Link, useHydrated, useRouterState } from "@tanstack/react-router";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@verself/ui/components/ui/sidebar";
import { cn } from "@verself/ui/lib/utils";
import { isPathActive, type NavEntry } from "./nav-config";
import { orgPath, useOrgRouteParams, type OrgScopedPath } from "./org-routing";

export function SidebarNavGroup({
  anchor,
  entries,
}: {
  readonly anchor?: "bottom";
  readonly entries: readonly NavEntry[];
}) {
  return (
    <SidebarGroup className={cn(anchor === "bottom" && "mt-auto")}>
      <SidebarGroupContent>
        <SidebarNavMenu entries={entries} />
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

function SidebarNavMenu({ entries }: { readonly entries: readonly NavEntry[] }) {
  const hydrated = useHydrated();
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <SidebarMenu>
      {entries.map((entry) => (
        <SidebarNavItem
          key={entry.id}
          entry={entry}
          path={path}
          tooltip={hydrated ? entry.label : undefined}
        />
      ))}
    </SidebarMenu>
  );
}

function SidebarNavItem({
  entry,
  path,
  tooltip,
}: {
  readonly entry: NavEntry;
  readonly path: string;
  readonly tooltip: string | undefined;
}) {
  const Icon = entry.icon;
  const { orgSlug } = useOrgRouteParams();
  const to = entry.id === "docs" ? entry.to : orgPath(orgSlug, entry.to as OrgScopedPath);

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isPathActive(path, entry)}
        className="bg-transparent data-active:bg-sidebar-accent"
        {...(tooltip ? { tooltip } : {})}
        render={
          <Link to={to} data-testid={`nav-${entry.id}`}>
            <Icon />
            <span>{entry.label}</span>
          </Link>
        }
      />
    </SidebarMenuItem>
  );
}
