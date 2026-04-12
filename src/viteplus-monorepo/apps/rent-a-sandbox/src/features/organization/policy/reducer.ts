import type { PolicyDocument, PolicyRole } from "~/lib/identity-api";

export type PermissionSet = ReadonlySet<string>;

export interface PolicyFormRole {
  readonly roleKey: string;
  readonly displayName: string;
  readonly permissions: PermissionSet;
}

export interface PolicyFormState {
  readonly version: number;
  readonly roles: readonly PolicyFormRole[];
}

export type PolicyAction = {
  readonly type: "set";
  readonly roleIndex: number;
  readonly permissions: readonly string[];
  readonly checked: boolean;
};

export function policyFormFromDocument(document: PolicyDocument): PolicyFormState {
  return {
    version: document.version,
    roles: document.roles.map((role) => ({
      roleKey: role.role_key,
      displayName: role.display_name,
      permissions: new Set(role.permissions ?? []),
    })),
  };
}

export function policyFormToRoles(state: PolicyFormState): Array<PolicyRole> {
  return state.roles.map((role) => ({
    role_key: role.roleKey,
    display_name: role.displayName,
    permissions: [...role.permissions].sort(),
  }));
}

export function policyReducer(state: PolicyFormState, action: PolicyAction): PolicyFormState {
  if (action.type !== "set") {
    return state;
  }
  if (action.roleIndex < 0 || action.roleIndex >= state.roles.length) {
    return state;
  }
  if (action.permissions.length === 0) {
    return state;
  }

  const target = state.roles[action.roleIndex];
  if (target === undefined) return state;
  const next = new Set(target.permissions);
  let mutated = false;
  if (action.checked) {
    for (const permission of action.permissions) {
      if (!next.has(permission)) {
        next.add(permission);
        mutated = true;
      }
    }
  } else {
    for (const permission of action.permissions) {
      if (next.has(permission)) {
        next.delete(permission);
        mutated = true;
      }
    }
  }
  if (!mutated) {
    return state;
  }

  return {
    version: state.version,
    roles: state.roles.map((role, index) =>
      index === action.roleIndex ? { ...role, permissions: next } : role,
    ),
  };
}

export function rolePermissionsEqual(a: PermissionSet, b: PermissionSet): boolean {
  if (a === b) return true;
  if (a.size !== b.size) return false;
  for (const value of a) {
    if (!b.has(value)) return false;
  }
  return true;
}

export function policyFormEqual(a: PolicyFormState, b: PolicyFormState): boolean {
  if (a === b) return true;
  if (a.version !== b.version) return false;
  if (a.roles.length !== b.roles.length) return false;
  for (let i = 0; i < a.roles.length; i += 1) {
    const left = a.roles[i];
    const right = b.roles[i];
    if (left === undefined || right === undefined) return false;
    if (left.roleKey !== right.roleKey) return false;
    if (left.displayName !== right.displayName) return false;
    if (!rolePermissionsEqual(left.permissions, right.permissions)) return false;
  }
  return true;
}
