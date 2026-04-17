// Single source of truth for the /policy left rail. Same shape as
// DOCS_NAV so the rail component can stay symmetrical.
export type PolicyNavChild = {
  readonly id: string;
  readonly label: string;
};

export type PolicyNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly exactMatch?: boolean;
  readonly children?: readonly PolicyNavChild[];
};

export const DATA_RETENTION_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "scope", label: "Scope & definitions" },
  { id: "lifecycle", label: "Account lifecycle" },
  { id: "retention", label: "Retention windows" },
  { id: "export", label: "Data export" },
  { id: "extensions", label: "Extensions" },
  { id: "deletion", label: "Final deletion" },
  { id: "operator", label: "Operator handling" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

export const POLICY_NAV: readonly PolicyNavEntry[] = [
  {
    id: "overview",
    label: "Overview",
    to: "/policy",
    matchPrefix: "/policy",
    exactMatch: true,
  },
  {
    id: "data-retention",
    label: "Data retention",
    to: "/policy/data-retention",
    matchPrefix: "/policy/data-retention",
    children: DATA_RETENTION_SECTIONS,
  },
];

export function isPathActive(
  currentPath: string,
  entry: { matchPrefix: string; exactMatch?: boolean },
): boolean {
  if (entry.exactMatch) {
    return currentPath === entry.matchPrefix;
  }
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
