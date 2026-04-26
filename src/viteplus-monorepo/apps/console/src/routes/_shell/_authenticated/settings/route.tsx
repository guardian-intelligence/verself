import { useMemo, useState } from "react";
import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { ChevronDown, ChevronLeft, Search } from "lucide-react";
import { cn } from "@verself/ui/lib/utils";
import { Input } from "@verself/ui/components/ui/input";
import {
  Page,
  PageEyebrow,
  PageHeader,
  PageHeaderContent,
  PageTitle,
} from "@verself/ui/components/ui/page";
import {
  isPathActive,
  SETTINGS_NAV,
  SETTINGS_NAV_GROUPS,
  type SettingsNavEntry,
  type SettingsNavGroup,
} from "~/features/shell/nav-config";

export const Route = createFileRoute("/_shell/_authenticated/settings")({
  component: SettingsLayout,
});

// Secondary layout for the settings subtree. The left rail is a curated
// secondary nav: a "Back" exit, a search-as-filter, and collapsible groups
// (Account / Membership / Billing). The content column carries a
// "Settings / <subpage>" eyebrow + the subpage title; child routes render
// their own PageSections directly into the Outlet.
function SettingsLayout() {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const activeEntry = resolveActiveEntry(path);
  const [query, setQuery] = useState("");

  const groups = useMemo(() => filterGroups(SETTINGS_NAV_GROUPS, query), [query]);

  return (
    <Page>
      <div className="flex flex-col gap-8 md:flex-row md:gap-10">
        <SettingsRail activePath={path} groups={groups} query={query} onQueryChange={setQuery} />

        <div className="min-w-0 flex-1">
          <PageHeader>
            <PageHeaderContent>
              <PageEyebrow>
                <Link to="/settings" className="text-muted-foreground/80 hover:text-foreground">
                  Settings
                </Link>
                {activeEntry ? (
                  <>
                    <span aria-hidden="true" className="text-muted-foreground/50">
                      /
                    </span>
                    <span className="text-foreground">{activeEntry.label}</span>
                  </>
                ) : null}
              </PageEyebrow>
              <PageTitle>{activeEntry?.label ?? "Settings"}</PageTitle>
            </PageHeaderContent>
          </PageHeader>

          <div className="mt-8">
            <Outlet />
          </div>
        </div>
      </div>
    </Page>
  );
}

function SettingsRail({
  activePath,
  groups,
  query,
  onQueryChange,
}: {
  readonly activePath: string;
  readonly groups: readonly SettingsNavGroup[];
  readonly query: string;
  readonly onQueryChange: (value: string) => void;
}) {
  return (
    <nav aria-label="Settings sections" className="flex flex-col gap-3 md:w-56 md:shrink-0">
      <Link
        to="/builds"
        className="flex items-center gap-1.5 self-start text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
        data-testid="settings-back"
      >
        <ChevronLeft className="size-3.5" />
        Back
      </Link>

      <div className="relative">
        <Search
          aria-hidden="true"
          className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          placeholder="Search settings…"
          className="h-8 pl-8 text-sm"
          data-testid="settings-search"
          aria-label="Search settings"
        />
      </div>

      <ul className="flex flex-col gap-4">
        {groups.map((group) => (
          <SettingsRailGroup key={group.id} group={group} activePath={activePath} />
        ))}
        {groups.length === 0 ? (
          <li className="px-2 text-xs text-muted-foreground">No matches.</li>
        ) : null}
      </ul>
    </nav>
  );
}

function SettingsRailGroup({
  group,
  activePath,
}: {
  readonly group: SettingsNavGroup;
  readonly activePath: string;
}) {
  // Each group is collapsible — defaults to expanded and persists nothing
  // across loads; the visual disclosure is the point, not the storage.
  const [open, setOpen] = useState(true);
  const containsActive = group.entries.some((entry) => isPathActive(activePath, entry));
  const expanded = open || containsActive;

  return (
    <li className="flex flex-col gap-1">
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        className="flex items-center gap-1 px-2 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/80 transition-colors hover:text-foreground"
        aria-expanded={expanded}
        data-testid={`settings-group-${group.id}`}
      >
        {group.label}
        <ChevronDown
          className={cn(
            "size-3 transition-transform duration-150",
            expanded ? "rotate-0" : "-rotate-90",
          )}
        />
      </button>
      {expanded ? (
        <ul className="flex flex-col">
          {group.entries.map((entry) => (
            <SettingsRailItem key={entry.id} entry={entry} activePath={activePath} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

function SettingsRailItem({
  entry,
  activePath,
}: {
  readonly entry: SettingsNavEntry;
  readonly activePath: string;
}) {
  const active = isPathActive(activePath, entry);
  return (
    <li>
      <Link
        to={entry.to}
        data-testid={`settings-tab-${entry.id}`}
        data-status={active ? "active" : "inactive"}
        className={cn(
          "block rounded-md px-2 py-1.5 text-sm transition-colors",
          active
            ? "bg-accent font-medium text-accent-foreground"
            : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
        )}
      >
        {entry.label}
      </Link>
    </li>
  );
}

function resolveActiveEntry(currentPath: string): SettingsNavEntry | null {
  return SETTINGS_NAV.find((entry) => isPathActive(currentPath, entry)) ?? null;
}

function filterGroups(
  groups: readonly SettingsNavGroup[],
  query: string,
): readonly SettingsNavGroup[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return groups;
  return groups
    .map((group) => ({
      ...group,
      entries: group.entries.filter(
        (entry) =>
          entry.label.toLowerCase().includes(needle) || group.label.toLowerCase().includes(needle),
      ),
    }))
    .filter((group) => group.entries.length > 0);
}
