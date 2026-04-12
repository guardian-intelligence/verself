import fc from "fast-check";
import { describe, expect, it } from "vite-plus/test";
import {
  allLeaves,
  buildCatalogTree,
  findNode,
  humanizeSegment,
  type CatalogNode,
  type GroupNode,
  type LeafNode,
} from "./catalog.ts";
import { fixtureOperations, knownPermissions } from "./test-fixtures.ts";

const tree = buildCatalogTree(fixtureOperations);

function collectGroups(root: GroupNode): GroupNode[] {
  const out: GroupNode[] = [];
  const walk = (node: CatalogNode): void => {
    if (node.kind !== "group") return;
    out.push(node);
    for (const child of node.children) walk(child);
  };
  walk(root);
  return out;
}

describe("humanizeSegment", () => {
  it("title-cases simple words", () => {
    expect(humanizeSegment("organization")).toBe("Organization");
    expect(humanizeSegment("policy")).toBe("Policy");
  });
  it("expands underscored multi-word segments", () => {
    expect(humanizeSegment("webhook_endpoint")).toBe("Webhook Endpoint");
  });
  it("uppercases known acronyms", () => {
    expect(humanizeSegment("api_credentials")).toBe("API Credentials");
  });
  it("returns empty string unchanged", () => {
    expect(humanizeSegment("")).toBe("");
  });
});

describe("buildCatalogTree", () => {
  it("dedupes operations sharing a permission into a single leaf", () => {
    const repoRead = findNode(tree, ["sandbox", "repo", "read"]);
    expect(repoRead?.kind).toBe("leaf");
    if (repoRead?.kind !== "leaf") return;
    const ops = repoRead.operations.map((operation) => operation.operation_id);
    expect(ops).toEqual(["get-repo", "list-repos"]);
  });

  it("dedupes the 4-row billing:read leak into one leaf with four ops", () => {
    const billingRead = findNode(tree, ["billing", "read"]);
    expect(billingRead?.kind).toBe("leaf");
    if (billingRead?.kind !== "leaf") return;
    expect(billingRead.operations.map((operation) => operation.operation_id).sort()).toEqual([
      "get-billing-balance",
      "get-billing-statement",
      "list-billing-grants",
      "list-billing-subscriptions",
    ]);
  });

  it("renders top-level groups in alphabetical order", () => {
    expect(tree.children.map((child) => child.segment)).toEqual(["billing", "identity", "sandbox"]);
  });

  it("descendantPermissions on every group is exactly the union of its leaves", () => {
    for (const group of collectGroups(tree)) {
      const leafPerms = new Set<string>();
      const walk = (node: CatalogNode): void => {
        if (node.kind === "leaf") {
          leafPerms.add(node.permission);
          return;
        }
        for (const child of node.children) walk(child);
      };
      walk(group);
      expect(new Set(group.descendantPermissions)).toEqual(leafPerms);
    }
  });

  it("descendantPermissions is deduped (each permission appears at most once)", () => {
    for (const group of collectGroups(tree)) {
      const set = new Set(group.descendantPermissions);
      expect(set.size).toBe(group.descendantPermissions.length);
    }
  });

  it("every leaf permission is in the known catalog and vice versa", () => {
    const leafPermissions = allLeaves(tree).map((leaf) => leaf.permission);
    expect(new Set(leafPermissions)).toEqual(new Set(knownPermissions));
  });

  it("every leaf path is the colon-split of its permission", () => {
    for (const leaf of allLeaves(tree)) {
      expect(leaf.path.join(":")).toBe(leaf.permission);
    }
  });
});

describe("buildCatalogTree property invariants", () => {
  it("findNode round-trips for every leaf", () => {
    fc.assert(
      fc.property(fc.constantFrom(...allLeaves(tree)), (leaf: LeafNode) => {
        const found = findNode(tree, leaf.path);
        return found?.kind === "leaf" && found.permission === leaf.permission;
      }),
    );
  });

  it("rebuilding from a shuffled operations list produces the same tree shape", () => {
    fc.assert(
      fc.property(fc.shuffledSubarray([...fixtureOperations.services]), (services) => {
        if (services.length === 0) return true;
        const reshuffled = { services };
        const rebuilt = buildCatalogTree(reshuffled);
        const rebuiltLeaves = new Set(allLeaves(rebuilt).map((leaf) => leaf.permission));
        const originalLeaves = new Set(
          services.flatMap((service) =>
            service.operations.map((operation) => operation.permission),
          ),
        );
        return setsEqual(rebuiltLeaves, originalLeaves);
      }),
    );
  });

  it("throws when a permission string is a strict prefix of another", () => {
    expect(() =>
      buildCatalogTree({
        services: [
          {
            service: "test",
            operations: [
              {
                operation_id: "a",
                permission: "foo:bar",
                resource: "x",
                action: "x",
                org_scope: "",
              },
              {
                operation_id: "b",
                permission: "foo:bar:baz",
                resource: "x",
                action: "x",
                org_scope: "",
              },
            ],
          },
        ],
      }),
    ).toThrow(/conflicts with leaf/);
  });

  it("throws on empty permission segments", () => {
    expect(() =>
      buildCatalogTree({
        services: [
          {
            service: "test",
            operations: [
              {
                operation_id: "a",
                permission: "foo::baz",
                resource: "x",
                action: "x",
                org_scope: "",
              },
            ],
          },
        ],
      }),
    ).toThrow(/invalid permission/);
  });
});

function setsEqual<T>(a: ReadonlySet<T>, b: ReadonlySet<T>): boolean {
  if (a.size !== b.size) return false;
  for (const value of a) if (!b.has(value)) return false;
  return true;
}
