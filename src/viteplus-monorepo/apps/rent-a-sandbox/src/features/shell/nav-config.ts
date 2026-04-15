import { Settings, Terminal, type LucideIcon } from "lucide-react";

// Single source of truth for everything the app shell advertises.
// Keep the shell UI, the command palette, and any programmatic route
// gating (e.g. "is this route a settings subpage") keyed on this manifest
// instead of fanning out hard-coded arrays across files.

export type NavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly icon: LucideIcon;
};

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
    id: "executions",
    label: "Executions",
    to: "/executions",
    matchPrefix: "/executions",
    icon: Terminal,
  },
] as const;

// Evergreen (non-product) rail entries anchored to the bottom of the
// sidebar above the account row. Currently just Settings; add future
// entries like Support or Status here.
export const EVERGREEN_NAV: readonly NavEntry[] = [
  {
    id: "settings",
    label: "Settings",
    to: "/settings",
    matchPrefix: "/settings",
    icon: Settings,
  },
] as const;

// Settings subpages. Rendered as the internal left-nav of the settings
// subtree, not as sidebar rows.
export const SETTINGS_NAV: readonly SettingsNavEntry[] = [
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
] as const;

export function isPathActive(currentPath: string, entry: { matchPrefix: string }): boolean {
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
