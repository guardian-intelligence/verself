import type { Operation, Operations } from "../../types.ts";

export type Segment = string;
export type Path = readonly Segment[];

interface CatalogNodeBase {
  readonly path: Path;
  readonly segment: Segment;
  readonly displayName: string;
}

export interface LeafNode extends CatalogNodeBase {
  readonly kind: "leaf";
  readonly permission: string;
  readonly operations: readonly LeafOperation[];
}

export interface GroupNode extends CatalogNodeBase {
  readonly kind: "group";
  readonly children: readonly CatalogNode[];
  readonly descendantPermissions: readonly string[];
}

export type CatalogNode = LeafNode | GroupNode;

export interface LeafOperation {
  readonly service: string;
  readonly operation_id: string;
  readonly resource: string;
  readonly action: string;
}

// Capitalized acronyms expanded after the segment-level title-case pass.
// Add to this set when humanize() produces something the operator would
// notice as wrong (e.g. "Api" → "API").
const ACRONYMS = new Set(["api", "id", "vm", "url", "io"]);

export function humanizeSegment(segment: Segment): string {
  if (segment.length === 0) return segment;
  const words = segment.split("_");
  return words
    .map((word) => {
      if (word.length === 0) return word;
      if (ACRONYMS.has(word.toLowerCase())) return word.toUpperCase();
      const first = word.charAt(0);
      return first.toUpperCase() + word.slice(1);
    })
    .join(" ");
}

interface MutableGroup {
  readonly path: string[];
  readonly children: Map<Segment, MutableGroup | MutableLeaf>;
}

interface MutableLeaf {
  readonly path: string[];
  readonly permission: string;
  readonly operations: LeafOperation[];
}

function isMutableLeaf(node: MutableGroup | MutableLeaf): node is MutableLeaf {
  return "permission" in node;
}

function flattenOperations(operations: Operations): Array<{ service: string; op: Operation }> {
  return operations.services.flatMap((service) =>
    service.operations.map((op) => ({ service: service.service, op })),
  );
}

function insert(root: MutableGroup, permission: string, service: string, op: Operation): void {
  const segments = permission.split(":");
  if (segments.length === 0 || segments.some((segment) => segment.length === 0)) {
    throw new Error(`policy/catalog: invalid permission string ${JSON.stringify(permission)}`);
  }

  let cursor: MutableGroup = root;
  for (let i = 0; i < segments.length - 1; i += 1) {
    const segment = segments[i] ?? "";
    const existing = cursor.children.get(segment);
    if (existing === undefined) {
      const child: MutableGroup = {
        path: [...cursor.path, segment],
        children: new Map(),
      };
      cursor.children.set(segment, child);
      cursor = child;
      continue;
    }
    if (isMutableLeaf(existing)) {
      throw new Error(
        `policy/catalog: permission ${JSON.stringify(permission)} conflicts with leaf ${JSON.stringify(existing.permission)} — a permission string is a prefix of another`,
      );
    }
    cursor = existing;
  }

  const leafSegment = segments[segments.length - 1] ?? "";
  const existing = cursor.children.get(leafSegment);
  const leafOp: LeafOperation = {
    service,
    operation_id: op.operation_id,
    resource: op.resource,
    action: op.action,
  };

  if (existing === undefined) {
    const leaf: MutableLeaf = {
      path: [...cursor.path, leafSegment],
      permission,
      operations: [leafOp],
    };
    cursor.children.set(leafSegment, leaf);
    return;
  }
  if (!isMutableLeaf(existing)) {
    throw new Error(
      `policy/catalog: permission ${JSON.stringify(permission)} conflicts with group at the same path — a group exists where a leaf is required`,
    );
  }
  existing.operations.push(leafOp);
}

function freezeGroup(group: MutableGroup, displayName: string): GroupNode {
  const children: CatalogNode[] = [];
  const sortedKeys = [...group.children.keys()].sort();
  const descendantPermissions: string[] = [];

  for (const key of sortedKeys) {
    const child = group.children.get(key);
    if (child === undefined) continue;
    if (isMutableLeaf(child)) {
      const leaf: LeafNode = {
        kind: "leaf",
        path: child.path,
        segment: key,
        displayName: humanizeSegment(key),
        permission: child.permission,
        operations: [...child.operations].sort((a, b) =>
          a.operation_id.localeCompare(b.operation_id),
        ),
      };
      children.push(leaf);
      descendantPermissions.push(leaf.permission);
      continue;
    }
    const frozenChild = freezeGroup(child, humanizeSegment(key));
    children.push(frozenChild);
    descendantPermissions.push(...frozenChild.descendantPermissions);
  }

  // descendantPermissions are pushed in catalog (sorted-segment) order; this
  // gives stable ordering across renders without re-sorting on every read.

  return {
    kind: "group",
    path: group.path,
    segment: group.path[group.path.length - 1] ?? "",
    displayName,
    children,
    descendantPermissions,
  };
}

export function buildCatalogTree(operations: Operations): GroupNode {
  const root: MutableGroup = { path: [], children: new Map() };
  const seen = new Set<string>();

  for (const { service, op } of flattenOperations(operations)) {
    seen.add(op.permission);
    insert(root, op.permission, service, op);
  }

  // Even if the service returns zero operations, we still want a stable empty
  // root so consumers can render an "empty catalog" state without null checks.
  return freezeGroup(root, "");
}

export function findNode(root: GroupNode, path: Path): CatalogNode | undefined {
  if (path.length === 0) return root;
  let cursor: CatalogNode = root;
  for (const segment of path) {
    if (cursor.kind !== "group") return undefined;
    const child: CatalogNode | undefined = cursor.children.find(
      (candidate) => candidate.segment === segment,
    );
    if (child === undefined) return undefined;
    cursor = child;
  }
  return cursor;
}

export function allLeaves(root: GroupNode): readonly LeafNode[] {
  const out: LeafNode[] = [];
  const walk = (node: CatalogNode): void => {
    if (node.kind === "leaf") {
      out.push(node);
      return;
    }
    for (const child of node.children) walk(child);
  };
  walk(root);
  return out;
}
