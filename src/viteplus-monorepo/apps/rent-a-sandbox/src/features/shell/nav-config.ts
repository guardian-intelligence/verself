// Single source of truth for everything the app shell advertises.
// Keep the shell UI, the command palette, and any programmatic route
// gating (e.g. "is this route a settings subpage") keyed on this manifest
// instead of fanning out hard-coded arrays across files.

export type ProductNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly shortcutHint: string;
};

export type SettingsNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
};

// Products live in the sidebar. Today there is exactly one; new product
// surfaces (Workloads, Long-lived VMs, Observability) slot in here when
// they land without touching the shell layout.
export const PRODUCT_NAV: readonly ProductNavEntry[] = [
  {
    id: "executions",
    label: "Executions",
    to: "/executions",
    matchPrefix: "/executions",
    shortcutHint: "G E",
  },
] as const;

// Settings subpages. The settings route is a separate route layout that
// renders this list as a horizontal tab strip on desktop and a select on
// mobile. Order here is the display order.
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
