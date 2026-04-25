import { BookOpen, CalendarClock, Settings, Terminal, type LucideIcon } from "lucide-react";

// Single source of truth for everything the app shell advertises.
// Keep the shell UI, the command palette, and any programmatic route
// gating (e.g. "is this route a settings subpage") keyed on this manifest
// instead of fanning out hard-coded arrays across files.

// Internal destinations navigate within this app via TanStack Router.
// External destinations open in a new tab — used for the Docs link that
// now points at the standalone platform.<domain> docs site.
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

// Product surfaces live at the top of the sidebar. Today there is exactly
// one; new products (Workloads, Long-lived VMs, Observability) slot in
// here without touching the shell layout.
export const PRIMARY_NAV: readonly NavEntry[] = [
  {
    kind: "internal",
    id: "executions",
    label: "Executions",
    to: "/executions",
    matchPrefix: "/executions",
    icon: Terminal,
  },
  {
    kind: "internal",
    id: "schedules",
    label: "Schedules",
    to: "/schedules",
    matchPrefix: "/schedules",
    icon: CalendarClock,
  },
];

// Evergreen (non-product) rail entries anchored to the bottom of the
// sidebar above the account row. Docs is served by the standalone
// platform.<domain> site, so it's an external nav entry — clicking opens
// a new tab rather than navigating the product shell away from the
// customer's workflow. Settings is gated by the underlying route, but we
// still surface the link to everyone: clicking it while signed out
// triggers the Zitadel flow via the _shell/_authenticated layout,
// matching the "no disabled buttons" rule.
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

// Settings subpages. Rendered as the internal left-nav of the settings
// subtree, not as sidebar rows.
export const SETTINGS_NAV: readonly SettingsNavEntry[] = [
  {
    id: "profile",
    label: "Profile",
    to: "/settings/profile",
    matchPrefix: "/settings/profile",
  },
  {
    id: "billing",
    label: "Billing",
    to: "/settings/billing",
    matchPrefix: "/settings/billing",
  },
  {
    id: "organization",
    label: "Organization",
    to: "/settings/organization",
    matchPrefix: "/settings/organization",
  },
  {
    id: "governance",
    label: "Governance",
    to: "/settings/governance",
    matchPrefix: "/settings/governance",
  },
];

export function isPathActive(currentPath: string, entry: { matchPrefix: string }): boolean {
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
