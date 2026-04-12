import fc from "fast-check";
import { describe, expect, it } from "vite-plus/test";
import { allLeaves, buildCatalogTree, findNode, type CatalogNode, type GroupNode } from "./catalog";
import { deriveTree, findRenderNode, type RenderNode } from "./derive";
import { fixtureOperations } from "./test-fixtures";

const catalog = buildCatalogTree(fixtureOperations);
const allLeafPermissions = allLeaves(catalog).map((leaf) => leaf.permission);

function descendantPathsOf(root: GroupNode): readonly (readonly string[])[] {
  const out: Array<readonly string[]> = [[]];
  const walk = (node: CatalogNode): void => {
    if (node.kind !== "group") return;
    out.push(node.path);
    for (const child of node.children) walk(child);
  };
  walk(root);
  return out;
}

const allPaths = descendantPathsOf(catalog);
const arbPermissions = fc.subarray(allLeafPermissions).map((permissions) => new Set(permissions));

describe("deriveTree — invariant 8: derive/state agreement", () => {
  it("group state is on iff every descendant is on, off iff none, mixed otherwise", () => {
    fc.assert(
      fc.property(
        arbPermissions,
        fc.constantFrom<readonly string[]>(...allPaths),
        (state, path) => {
          const tree = deriveTree(catalog, state);
          const renderNode = findRenderNode(tree, path);
          const catalogNode = findNode(catalog, path);
          if (renderNode === undefined || catalogNode === undefined) return false;
          if (catalogNode.kind === "leaf") {
            const expected = state.has(catalogNode.permission) ? "on" : "off";
            return renderNode.kind === "leaf" && renderNode.state === expected;
          }
          if (renderNode.kind !== "group") return false;
          const descendants = catalogNode.descendantPermissions;
          const allOn = descendants.every((permission) => state.has(permission));
          const noneOn = descendants.every((permission) => !state.has(permission));
          if (allOn) return renderNode.state === "on";
          if (noneOn) return renderNode.state === "off";
          return renderNode.state === "mixed";
        },
      ),
    );
  });
});

describe("deriveTree — purity", () => {
  it("equal state inputs produce structurally equal trees", () => {
    fc.assert(
      fc.property(fc.subarray(allLeafPermissions), (permissions) => {
        const a = deriveTree(catalog, new Set(permissions));
        const b = deriveTree(catalog, new Set(permissions));
        return JSON.stringify(serialize(a)) === JSON.stringify(serialize(b));
      }),
    );
  });
});

describe("deriveTree — bug repro: independent leaves do not co-toggle", () => {
  // The original PolicyEditor bug: ticking import-repo (sandbox:repo:write)
  // also visually checked rescan-repo. Verify the new pipeline keeps both
  // visual representations of any pair of distinct leaves independent.
  it("toggling sandbox:repo:write does not change sandbox:repo:read render state", () => {
    const tree = deriveTree(catalog, new Set(["sandbox:repo:write"]));
    const write = findRenderNode(tree, ["sandbox", "repo", "write"]);
    const read = findRenderNode(tree, ["sandbox", "repo", "read"]);
    expect(write?.kind).toBe("leaf");
    expect(read?.kind).toBe("leaf");
    if (write?.kind !== "leaf" || read?.kind !== "leaf") return;
    expect(write.state).toBe("on");
    expect(read.state).toBe("off");
  });

  it("toggling billing:read does not affect billing:checkout", () => {
    const tree = deriveTree(catalog, new Set(["billing:read"]));
    const billing = findRenderNode(tree, ["billing"]);
    expect(billing?.kind).toBe("group");
    if (billing?.kind !== "group") return;
    expect(billing.state).toBe("mixed");
  });

  it("filling all of sandbox renders the sandbox group as on", () => {
    const sandboxNode = findNode(catalog, ["sandbox"]);
    if (sandboxNode?.kind !== "group") throw new Error("expected sandbox group");
    const tree = deriveTree(catalog, new Set(sandboxNode.descendantPermissions));
    const sandboxRender = findRenderNode(tree, ["sandbox"]);
    expect(sandboxRender?.kind).toBe("group");
    if (sandboxRender?.kind !== "group") return;
    expect(sandboxRender.state).toBe("on");
  });

  it("the empty set renders the root as off", () => {
    const tree = deriveTree(catalog, new Set());
    expect(tree.state).toBe("off");
  });

  it("the full set renders the root as on", () => {
    const tree = deriveTree(catalog, new Set(allLeafPermissions));
    expect(tree.state).toBe("on");
  });
});

function serialize(node: RenderNode): unknown {
  if (node.kind === "leaf") {
    return { kind: "leaf", permission: node.permission, state: node.state };
  }
  return {
    kind: "group",
    segment: node.segment,
    state: node.state,
    children: node.children.map((child) => serialize(child)),
  };
}
