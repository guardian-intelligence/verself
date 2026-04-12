import fc from "fast-check";
import { describe, expect, it } from "vite-plus/test";
import {
  allLeaves,
  buildCatalogTree,
  findNode,
  type CatalogNode,
  type GroupNode,
} from "./catalog.ts";
import {
  policyFormEqual,
  policyFormFromDocument,
  policyFormToRoles,
  policyReducer,
  rolePermissionsEqual,
  type PolicyAction,
  type PolicyFormState,
} from "./reducer.ts";
import { fixtureOperations, knownPermissions } from "./test-fixtures.ts";

const catalog = buildCatalogTree(fixtureOperations);

function makeForm(...rolePermissions: ReadonlyArray<readonly string[]>): PolicyFormState {
  return {
    version: 0,
    roles: rolePermissions.map((permissions, index) => ({
      roleKey: `role_${index}`,
      displayName: `Role ${index}`,
      permissions: new Set(permissions),
    })),
  };
}

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

function descendantPermissionsAt(path: readonly string[]): readonly string[] {
  const node = findNode(catalog, path);
  if (node === undefined) return [];
  if (node.kind === "leaf") return [node.permission];
  return node.descendantPermissions;
}

const allPaths = descendantPathsOf(catalog);
const allLeafPermissions = allLeaves(catalog).map((leaf) => leaf.permission);
const arbPermission = fc.constantFrom(...knownPermissions);
const arbPath = fc.constantFrom<readonly string[]>(...allPaths);
const arbPermissionSet = fc.subarray(allLeafPermissions);
const arbInitialForm = fc
  .array(arbPermissionSet, { minLength: 1, maxLength: 4 })
  .map((rolePermissionsList) => makeForm(...rolePermissionsList));
const arbAction = (state: PolicyFormState): fc.Arbitrary<PolicyAction> =>
  fc
    .tuple(fc.integer({ min: 0, max: state.roles.length - 1 }), arbPath, fc.boolean())
    .map(([roleIndex, path, checked]) => ({
      type: "set" as const,
      roleIndex,
      permissions: descendantPermissionsAt(path),
      checked,
    }))
    .filter((action) => action.permissions.length > 0);

describe("policyReducer — invariant 1: leaf independence", () => {
  it("setting one permission never changes any other permission's membership", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPermission,
            arbPermission,
            fc.boolean(),
          ),
        ),
        ([state, roleIndex, p, q, checked]) => {
          if (p === q) return true;
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: [p],
            checked,
          });
          return (
            next.roles[roleIndex]!.permissions.has(q) === state.roles[roleIndex]!.permissions.has(q)
          );
        },
      ),
    );
  });
});

describe("policyReducer — invariant 2: subtree completeness", () => {
  it("set(group, true) makes every descendant present; set(group, false) removes them all", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const permissions = descendantPermissionsAt(path);
          if (permissions.length === 0) return true;

          const turnedOn = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions,
            checked: true,
          });
          for (const permission of permissions) {
            if (!turnedOn.roles[roleIndex]!.permissions.has(permission)) return false;
          }

          const turnedOff = policyReducer(turnedOn, {
            type: "set",
            roleIndex,
            permissions,
            checked: false,
          });
          for (const permission of permissions) {
            if (turnedOff.roles[roleIndex]!.permissions.has(permission)) return false;
          }
          return true;
        },
      ),
    );
  });
});

describe("policyReducer — invariant 3: non-contamination", () => {
  it("setting permissions inside a subtree never changes membership of permissions outside it", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
            fc.boolean(),
          ),
        ),
        ([state, roleIndex, path, checked]) => {
          const subtree = new Set(descendantPermissionsAt(path));
          if (subtree.size === 0) return true;

          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: [...subtree],
            checked,
          });
          for (const permission of allLeafPermissions) {
            if (subtree.has(permission)) continue;
            const before = state.roles[roleIndex]!.permissions.has(permission);
            const after = next.roles[roleIndex]!.permissions.has(permission);
            if (before !== after) return false;
          }
          return true;
        },
      ),
    );
  });
});

describe("policyReducer — invariant 4: idempotence", () => {
  it("applying the same action twice equals applying it once", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) => fc.tuple(fc.constant(state), arbAction(state))),
        ([state, action]) => {
          const once = policyReducer(state, action);
          const twice = policyReducer(once, action);
          return policyFormEqual(once, twice);
        },
      ),
    );
  });
});

describe("policyReducer — invariant 5: monotonicity", () => {
  it("checked:true never shrinks the role's permission set", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const permissions = descendantPermissionsAt(path);
          if (permissions.length === 0) return true;
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions,
            checked: true,
          });
          for (const existing of state.roles[roleIndex]!.permissions) {
            if (!next.roles[roleIndex]!.permissions.has(existing)) return false;
          }
          return (
            next.roles[roleIndex]!.permissions.size >= state.roles[roleIndex]!.permissions.size
          );
        },
      ),
    );
  });

  it("checked:false never grows the role's permission set", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const permissions = descendantPermissionsAt(path);
          if (permissions.length === 0) return true;
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions,
            checked: false,
          });
          return (
            next.roles[roleIndex]!.permissions.size <= state.roles[roleIndex]!.permissions.size
          );
        },
      ),
    );
  });
});

describe("policyReducer — invariant 6: closure", () => {
  it("starting from a known-permissions state, no action can introduce an unknown permission", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) => fc.tuple(fc.constant(state), arbAction(state))),
        ([state, action]) => {
          const next = policyReducer(state, action);
          for (const role of next.roles) {
            for (const permission of role.permissions) {
              if (!knownPermissions.includes(permission)) return false;
            }
          }
          return true;
        },
      ),
    );
  });
});

describe("policyReducer — invariant 7: role isolation", () => {
  it("an action targeting roleIndex i never mutates any role at index j ≠ i (reference equality)", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) => fc.tuple(fc.constant(state), arbAction(state))),
        ([state, action]) => {
          const next = policyReducer(state, action);
          for (let i = 0; i < state.roles.length; i += 1) {
            if (i === action.roleIndex) continue;
            if (next.roles[i] !== state.roles[i]) return false;
          }
          return true;
        },
      ),
    );
  });
});

describe("policyReducer — invariant 9: symmetric leaf", () => {
  it("setting a leaf via [permission] equals setting that leaf via the singleton group form", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPermission,
            fc.boolean(),
          ),
        ),
        ([state, roleIndex, permission, checked]) => {
          const a = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: [permission],
            checked,
          });
          const b = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: [permission, permission],
            checked,
          });
          return rolePermissionsEqual(
            a.roles[roleIndex]!.permissions,
            b.roles[roleIndex]!.permissions,
          );
        },
      ),
    );
  });
});

describe("policyReducer — invariant 10: round-trip", () => {
  it("set(perms,true) then set(perms,false) leaves none of those perms in the role", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const permissions = descendantPermissionsAt(path);
          if (permissions.length === 0) return true;
          const turnedOn = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions,
            checked: true,
          });
          const turnedOff = policyReducer(turnedOn, {
            type: "set",
            roleIndex,
            permissions,
            checked: false,
          });
          for (const permission of permissions) {
            if (turnedOff.roles[roleIndex]!.permissions.has(permission)) return false;
          }
          return true;
        },
      ),
    );
  });
});

describe("policyFormFromDocument / policyFormToRoles", () => {
  it("round-trips a policy document", () => {
    const doc = {
      org_id: "1",
      version: 7,
      roles: [
        { role_key: "a", display_name: "A", permissions: ["billing:read", "sandbox:repo:read"] },
        { role_key: "b", display_name: "B", permissions: [] },
      ],
      updated_at: "2026-04-11T00:00:00Z",
      updated_by: "tester",
    };
    const form = policyFormFromDocument(doc);
    expect(form.version).toBe(7);
    expect(form.roles).toHaveLength(2);
    const back = policyFormToRoles(form);
    expect(back).toEqual([
      {
        role_key: "a",
        display_name: "A",
        permissions: ["billing:read", "sandbox:repo:read"],
      },
      { role_key: "b", display_name: "B", permissions: [] },
    ]);
  });
});

describe("policyReducer — invariant 11: clicking a mixed group fills the subtree", () => {
  // PolicyMatrix maps a click on a tri-state checkbox to
  // dispatch({ checked: true, permissions: descendantPermissions }). This
  // property is the canary for that contract: any group, however partially
  // populated, must end fully on after a single fill action.
  it("set(group.descendants, true) ends with every descendant on", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const descendants = descendantPermissionsAt(path);
          if (descendants.length === 0) return true;
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: descendants,
            checked: true,
          });
          const role = next.roles[roleIndex]!;
          return descendants.every((permission) => role.permissions.has(permission));
        },
      ),
    );
  });

  it("set(group.descendants, false) ends with every descendant off", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPath,
          ),
        ),
        ([state, roleIndex, path]) => {
          const descendants = descendantPermissionsAt(path);
          if (descendants.length === 0) return true;
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: descendants,
            checked: false,
          });
          const role = next.roles[roleIndex]!;
          return descendants.every((permission) => !role.permissions.has(permission));
        },
      ),
    );
  });
});

describe("policyReducer — return-state stability", () => {
  it("a no-op action returns the exact same state reference", () => {
    fc.assert(
      fc.property(
        arbInitialForm.chain((state) =>
          fc.tuple(
            fc.constant(state),
            fc.integer({ min: 0, max: state.roles.length - 1 }),
            arbPermission,
          ),
        ),
        ([state, roleIndex, permission]) => {
          const alreadyOn = state.roles[roleIndex]!.permissions.has(permission);
          const next = policyReducer(state, {
            type: "set",
            roleIndex,
            permissions: [permission],
            checked: alreadyOn,
          });
          return next === state;
        },
      ),
    );
  });
});
