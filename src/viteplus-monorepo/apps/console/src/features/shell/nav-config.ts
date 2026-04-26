import { BookOpen, CalendarClock, GitBranch, Settings, type LucideIcon } from "lucide-react";

// Single source of truth for everything the app shell advertises.
// Keep the shell UI, the command palette, and any programmatic route
// gating (e.g. "is this route a settings subpage") keyed on this manifest
// instead of fanning out hard-coded arrays across files.

// Internal destinations navigate within this app via TanStack Router.
// External destinations open in a new tab — used for the Docs link that
// points at the product apex docs site.
export type InternalNavEntry = {
  readonly kind: "internal";
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly icon: LucideIcon;
};

export type ExternalNavEntry = {
  readonly kind: "external";
  readonly id: string;
  readonly label: string;
  // Path portion ("/docs") — consumers join it with the runtime-resolved
  // platform origin so the link is portable across deployments.
  readonly path: string;
  readonly icon: LucideIcon;
};

export type NavEntry = InternalNavEntry | ExternalNavEntry;

export type SettingsNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
};

export type SettingsNavGroup = {
  readonly id: string;
  readonly label: string;
  readonly entries: readonly SettingsNavEntry[];
};

// Product surfaces live at the top of the sidebar. The rail is intentionally
// flat: two product surfaces only. Executions are reachable via Builds and
// Schedules detail pages (not via a top-level rail entry).
export const PRIMARY_NAV: readonly NavEntry[] = [
  {
    kind: "internal",
    id: "schedules",
    label: "Schedules",
    to: "/schedules",
    matchPrefix: "/schedules",
    icon: CalendarClock,
  },
  {
    kind: "internal",
    id: "builds",
    label: "Builds",
    to: "/builds",
    matchPrefix: "/builds",
    icon: GitBranch,
  },
];

// Evergreen (non-product) rail entries anchored to the bottom of the sidebar.
// Docs is served by the product apex, so it's an external nav entry — clicking
// opens a new tab rather than navigating the product shell away from the
// customer's workflow. Settings is gated by the underlying route, but we still
// surface the link to everyone: clicking it while signed out triggers the
// Zitadel flow via the _shell/_authenticated layout, matching the "no
// disabled buttons" rule.
export const EVERGREEN_NAV: readonly NavEntry[] = [
  {
    kind: "external",
    id: "docs",
    label: "Docs",
    path: "/docs",
    icon: BookOpen,
  },
  {
    kind: "internal",
    id: "settings",
    label: "Settings",
    to: "/settings",
    matchPrefix: "/settings",
    icon: Settings,
  },
];

// Settings subpages, grouped into the same "Account settings / Membership /
// Billing" buckets the rest of the industry has converged on. Group order
// matches the visual order of the secondary rail.
export const SETTINGS_NAV_GROUPS: readonly SettingsNavGroup[] = [
  {
    id: "account",
    label: "Account settings",
    entries: [
      {
        id: "profile",
        label: "Profile",
        to: "/settings/profile",
        matchPrefix: "/settings/profile",
      },
    ],
  },
  {
    id: "membership",
    label: "Membership",
    entries: [
      {
        id: "organization",
        label: "Members",
        to: "/settings/organization",
        matchPrefix: "/settings/organization",
      },
      {
        id: "governance",
        label: "Governance",
        to: "/settings/governance",
        matchPrefix: "/settings/governance",
      },
    ],
  },
  {
    id: "billing",
    label: "Billing",
    entries: [
      {
        id: "billing",
        label: "Plans & usage",
        to: "/settings/billing",
        matchPrefix: "/settings/billing",
      },
    ],
  },
];

// Flat view of the same settings entries. Used by anything that wants to
// iterate across all subpages (command palette, breadcrumb resolver) without
// caring about grouping.
export const SETTINGS_NAV: readonly SettingsNavEntry[] = SETTINGS_NAV_GROUPS.flatMap(
  (group) => group.entries,
);

export function isPathActive(currentPath: string, entry: { matchPrefix: string }): boolean {
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
