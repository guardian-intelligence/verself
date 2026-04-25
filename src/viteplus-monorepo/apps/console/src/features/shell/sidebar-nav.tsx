import { Link, useHydrated, useRouterState } from "@tanstack/react-router";
import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@forge-metal/ui/components/ui/sidebar";
import { cn } from "@forge-metal/ui";
import { isPathActive, type NavEntry } from "./nav-config";
import { useShellConfig } from "./shell-config";

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
  const { platformOrigin } = useShellConfig();

  return (
    <SidebarMenu>
      {entries.map((entry) => (
        <SidebarNavItem
          key={entry.id}
          entry={entry}
          path={path}
          platformOrigin={platformOrigin}
          tooltip={hydrated ? entry.label : undefined}
        />
      ))}
    </SidebarMenu>
  );
}

function SidebarNavItem({
  entry,
  path,
  platformOrigin,
  tooltip,
}: {
  readonly entry: NavEntry;
  readonly path: string;
  readonly platformOrigin: string;
  readonly tooltip: string | undefined;
}) {
  const Icon = entry.icon;

  if (entry.kind === "external") {
    const href = `${platformOrigin.replace(/\/$/, "")}${entry.path}`;
    return (
      <SidebarMenuItem>
        <SidebarMenuButton
          {...(tooltip ? { tooltip } : {})}
          render={
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              data-testid={`nav-${entry.id}`}
            >
              <Icon />
              <span>{entry.label}</span>
            </a>
          }
        />
      </SidebarMenuItem>
    );
  }

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        isActive={isPathActive(path, entry)}
        {...(tooltip ? { tooltip } : {})}
        render={
          <Link to={entry.to} data-testid={`nav-${entry.id}`}>
            <Icon />
            <span>{entry.label}</span>
          </Link>
        }
      />
    </SidebarMenuItem>
  );
}
