import type { CatalogNode, GroupNode, LeafNode, Path } from "./catalog.ts";
import type { PermissionSet } from "./reducer.ts";

export type NodeState = "off" | "on" | "mixed";

export interface RenderLeaf extends LeafNode {
  readonly state: "off" | "on";
}

export interface RenderGroup extends Omit<GroupNode, "children" | "descendantPermissions"> {
  readonly state: NodeState;
  readonly children: readonly RenderNode[];
  readonly descendantPermissions: readonly string[];
}

export type RenderNode = RenderLeaf | RenderGroup;

interface DeriveResult {
  readonly node: RenderNode;
  readonly onCount: number;
  readonly leafCount: number;
}

function deriveNode(node: CatalogNode, permissions: PermissionSet): DeriveResult {
  if (node.kind === "leaf") {
    const on = permissions.has(node.permission);
    return {
      node: { ...node, state: on ? "on" : "off" },
      onCount: on ? 1 : 0,
      leafCount: 1,
    };
  }

  const childResults = node.children.map((child) => deriveNode(child, permissions));
  const onCount = childResults.reduce((sum, result) => sum + result.onCount, 0);
  const leafCount = childResults.reduce((sum, result) => sum + result.leafCount, 0);

  let state: NodeState;
  if (leafCount === 0 || onCount === 0) {
    state = "off";
  } else if (onCount === leafCount) {
    state = "on";
  } else {
    state = "mixed";
  }

  return {
    node: {
      kind: "group",
      path: node.path,
      segment: node.segment,
      displayName: node.displayName,
      descendantPermissions: node.descendantPermissions,
      children: childResults.map((result) => result.node),
      state,
    },
    onCount,
    leafCount,
  };
}

export function deriveTree(catalog: GroupNode, permissions: PermissionSet): RenderGroup {
  const result = deriveNode(catalog, permissions);
  return result.node as RenderGroup;
}

export function findRenderNode(root: RenderGroup, path: Path): RenderNode | undefined {
  if (path.length === 0) return root;
  let cursor: RenderNode = root;
  for (const segment of path) {
    if (cursor.kind !== "group") return undefined;
    const child: RenderNode | undefined = cursor.children.find(
      (candidate) => candidate.segment === segment,
    );
    if (child === undefined) return undefined;
    cursor = child;
  }
  return cursor;
}
