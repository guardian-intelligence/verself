import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { cn } from "@verself/ui/lib/utils";
import { useActiveAnchor } from "@verself/ui/hooks/use-active-anchor";
import {
  POLICY_NAV,
  type PolicyNavChild,
  type PolicyNavEntry,
  isPathActive,
} from "~/lib/policy-nav";

export const Route = createFileRoute("/_workshop/policy")({
  component: PolicyLayout,
});

function PolicyLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <div className="mx-auto w-full max-w-7xl px-4 py-8 md:px-6 md:py-10">
      <div className="flex flex-col gap-8 md:flex-row md:items-start md:gap-10">
        <PolicyRail path={path} />
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  );
}

function PolicyRail({ path }: { path: string }) {
  const activeEntry = POLICY_NAV.find((entry) => isPathActive(path, entry));
  const activeChildren = activeEntry?.children ?? [];

  return (
    <nav
      aria-label="Policy sections"
      className="md:sticky md:top-[var(--header-scroll-offset)] md:w-56 md:shrink-0"
    >
      <div className="flex flex-col gap-2 md:gap-0.5">
        <ul className="flex gap-1 overflow-x-auto md:flex-col md:gap-0.5 md:overflow-visible">
          {POLICY_NAV.map((entry) => (
            <li key={entry.id}>
              <PolicyRailEntry entry={entry} path={path} />
              {entry.children && entry.children.length > 0 && isPathActive(path, entry) && (
                <PolicyRailChildren children={entry.children} mobile="desktop-only" />
              )}
            </li>
          ))}
        </ul>
        {activeChildren.length > 0 && (
          <PolicyRailChildren children={activeChildren} mobile="mobile-only" />
        )}
      </div>
    </nav>
  );
}

function PolicyRailEntry({ entry, path }: { entry: PolicyNavEntry; path: string }) {
  const active = isPathActive(path, entry);
  return (
    <Link
      to={entry.to}
      data-testid={`policy-nav-${entry.id}`}
      data-active={active ? "true" : "false"}
      className={cn(
        "block whitespace-nowrap rounded-md px-3 py-1.5 text-sm transition-colors",
        "data-[active=false]:text-muted-foreground data-[active=false]:hover:bg-accent/60 data-[active=false]:hover:text-foreground",
        "data-[active=true]:bg-accent data-[active=true]:font-medium data-[active=true]:text-foreground",
      )}
    >
      {entry.label}
    </Link>
  );
}

function PolicyRailChildren({
  children,
  mobile,
}: {
  children: readonly PolicyNavChild[];
  mobile: "desktop-only" | "mobile-only";
}) {
  const active = useActiveAnchor(children);

  if (mobile === "desktop-only") {
    return (
      <ul className="hidden md:ml-3 md:mt-1 md:flex md:flex-col md:gap-0 md:border-l md:border-border">
        {children.map((child) => (
          <li key={child.id}>
            <PolicyRailChildLink child={child} isActive={child.id === active} variant="rail" />
          </li>
        ))}
      </ul>
    );
  }

  return (
    <ul className="flex gap-1 overflow-x-auto px-3 md:hidden">
      {children.map((child) => (
        <li key={child.id}>
          <PolicyRailChildLink child={child} isActive={child.id === active} variant="chip" />
        </li>
      ))}
    </ul>
  );
}

function PolicyRailChildLink({
  child,
  isActive,
  variant,
}: {
  child: PolicyNavChild;
  isActive: boolean;
  variant: "rail" | "chip";
}) {
  return (
    <a
      href={`#${child.id}`}
      data-active={isActive ? "true" : "false"}
      data-testid={`policy-nav-child-${child.id}`}
      className={cn(
        variant === "rail"
          ? [
              "relative block whitespace-nowrap py-1.5 pl-3 pr-3 -ml-px text-sm transition-colors",
              "data-[active=false]:text-muted-foreground data-[active=false]:hover:text-foreground",
              "data-[active=true]:font-medium data-[active=true]:text-foreground",
              "data-[active=true]:before:absolute data-[active=true]:before:left-[-1px] data-[active=true]:before:top-1/2 data-[active=true]:before:h-4 data-[active=true]:before:w-px data-[active=true]:before:-translate-y-1/2 data-[active=true]:before:bg-foreground",
            ]
          : [
              "block whitespace-nowrap rounded-md px-3 py-1.5 text-sm transition-colors",
              "data-[active=false]:text-muted-foreground data-[active=false]:hover:bg-accent/60 data-[active=false]:hover:text-foreground",
              "data-[active=true]:bg-accent data-[active=true]:font-medium data-[active=true]:text-foreground",
            ],
      )}
    >
      {child.label}
    </a>
  );
}
