import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { cn } from "@forge-metal/ui/lib/utils";
import { useActiveAnchor } from "@forge-metal/ui/hooks/use-active-anchor";
import {
  DOCS_NAV,
  type DocsNavChild,
  type DocsNavEntry,
  isPathActive,
} from "~/lib/docs-nav";

export const Route = createFileRoute("/docs")({
  component: DocsLayout,
});

// Docs shell. One rail carries both levels:
//   Desktop: vertical stack, children nest under the active parent with a
//   hairline guide + left-edge tick on the active child.
//   Mobile: two stacked horizontal strips — top-level pages, then the
//   active parent's children below it. No right rail, no floating TOC.
//
// "Resting affordance" is the point of the design: you should be able to
// read the hierarchy and current selection at a glance without hovering.
// Active parent gets a muted fill + bold label; active child gets a bold
// label + a 1px accent tick that sits on the group's hairline so the eye
// connects "parent → child" as a single line.
function DocsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });

  return (
    <div className="mx-auto w-full max-w-7xl px-4 py-8 md:px-6 md:py-10">
      <div className="flex flex-col gap-8 md:flex-row md:items-start md:gap-10">
        <DocsRail path={path} />
        <div className="min-w-0 flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  );
}

function DocsRail({ path }: { path: string }) {
  const activeEntry = DOCS_NAV.find((entry) => isPathActive(path, entry));
  const activeChildren = activeEntry?.children ?? [];

  return (
    <nav
      aria-label="Docs sections"
      className="md:sticky md:top-[var(--header-scroll-offset)] md:w-56 md:shrink-0"
    >
      {/* Desktop: vertical rail. Mobile: stacked horizontal scrollers. */}
      <div className="flex flex-col gap-2 md:gap-0.5">
        <ul className="flex gap-1 overflow-x-auto md:flex-col md:gap-0.5 md:overflow-visible">
          {DOCS_NAV.map((entry) => (
            <li key={entry.id}>
              <DocsRailEntry entry={entry} path={path} />
              {entry.children && entry.children.length > 0 && isPathActive(path, entry) && (
                <DocsRailChildren children={entry.children} mobile="desktop-only" />
              )}
            </li>
          ))}
        </ul>
        {activeChildren.length > 0 && (
          <DocsRailChildren children={activeChildren} mobile="mobile-only" />
        )}
      </div>
    </nav>
  );
}

function DocsRailEntry({ entry, path }: { entry: DocsNavEntry; path: string }) {
  const active = isPathActive(path, entry);
  return (
    <Link
      to={entry.to}
      data-testid={`docs-nav-${entry.id}`}
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

function DocsRailChildren({
  children,
  mobile,
}: {
  children: readonly DocsNavChild[];
  mobile: "desktop-only" | "mobile-only";
}) {
  const active = useActiveAnchor(children);

  if (mobile === "desktop-only") {
    return (
      <ul className="hidden md:ml-3 md:mt-1 md:flex md:flex-col md:gap-0 md:border-l md:border-border">
        {children.map((child) => (
          <li key={child.id}>
            <DocsRailChildLink child={child} isActive={child.id === active} variant="rail" />
          </li>
        ))}
      </ul>
    );
  }

  return (
    <ul className="flex gap-1 overflow-x-auto px-3 md:hidden">
      {children.map((child) => (
        <li key={child.id}>
          <DocsRailChildLink child={child} isActive={child.id === active} variant="chip" />
        </li>
      ))}
    </ul>
  );
}

function DocsRailChildLink({
  child,
  isActive,
  variant,
}: {
  child: DocsNavChild;
  isActive: boolean;
  variant: "rail" | "chip";
}) {
  return (
    <a
      href={`#${child.id}`}
      data-active={isActive ? "true" : "false"}
      data-testid={`docs-nav-child-${child.id}`}
      className={cn(
        variant === "rail"
          ? [
              // Sits flush with the parent's hairline border so the active
              // tick overlaps it cleanly.
              "relative block whitespace-nowrap py-1.5 pl-3 pr-3 -ml-px text-sm transition-colors",
              "data-[active=false]:text-muted-foreground data-[active=false]:hover:text-foreground",
              "data-[active=true]:font-medium data-[active=true]:text-foreground",
              // 1px accent tick on top of the group's left rule.
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
