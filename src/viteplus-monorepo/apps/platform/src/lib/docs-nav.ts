import { REFERENCE_SECTIONS } from "./openapi-catalog";

// Single source of truth for the docs left rail. Top-level entries are
// doc pages; optional `children` are in-page anchors that expand under
// the active entry. Reference children are derived from the OpenAPI
// catalog so adding a service to SERVICE_CATALOG automatically surfaces
// it in the rail — no manual sync point.
export type DocsNavChild = {
  readonly id: string; // must match the DOM id the anchor scrolls to
  readonly label: string;
};

export type DocsNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly exactMatch?: boolean;
  readonly children?: readonly DocsNavChild[];
};

export const DOCS_NAV: readonly DocsNavEntry[] = [
  {
    id: "overview",
    label: "Overview",
    to: "/docs",
    matchPrefix: "/docs",
    exactMatch: true,
  },
  {
    id: "reference",
    label: "API Reference",
    to: "/docs/reference",
    matchPrefix: "/docs/reference",
    children: REFERENCE_SECTIONS,
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
