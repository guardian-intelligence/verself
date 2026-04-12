import { useEffect, useMemo, useRef } from "react";
import type { CatalogNode, GroupNode } from "./catalog";
import { deriveTree, findRenderNode, type NodeState } from "./derive";
import type { PolicyAction, PolicyFormRole, PolicyFormState } from "./reducer";

interface PolicyMatrixProps {
  readonly catalog: GroupNode;
  readonly state: PolicyFormState;
  readonly dispatch: (action: PolicyAction) => void;
  readonly canEdit: boolean;
}

export function PolicyMatrix({ catalog, state, dispatch, canEdit }: PolicyMatrixProps) {
  const renderTrees = useMemo(
    () => state.roles.map((role) => deriveTree(catalog, role.permissions)),
    [catalog, state.roles],
  );
  const rows = useMemo(() => flatten(catalog), [catalog]);

  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
        No service operations have been published yet.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50">
          <tr>
            <th scope="col" className="px-4 py-2 text-left font-medium">
              Permission
            </th>
            {state.roles.map((role) => (
              <th scope="col" key={role.roleKey} className="px-4 py-2 text-left font-medium">
                {role.displayName}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {rows.map((row) => (
            <PolicyRow
              key={row.node.path.join(":") || row.node.segment}
              row={row}
              roles={state.roles}
              renderTrees={renderTrees}
              dispatch={dispatch}
              canEdit={canEdit}
            />
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface FlatRow {
  readonly node: CatalogNode;
  readonly depth: number;
}

function flatten(root: GroupNode): readonly FlatRow[] {
  const out: FlatRow[] = [];
  const walk = (node: CatalogNode, depth: number) => {
    out.push({ node, depth });
    if (node.kind === "group") {
      for (const child of node.children) walk(child, depth + 1);
    }
  };
  for (const child of root.children) walk(child, 0);
  return out;
}

interface PolicyRowProps {
  readonly row: FlatRow;
  readonly roles: readonly PolicyFormRole[];
  readonly renderTrees: ReadonlyArray<ReturnType<typeof deriveTree>>;
  readonly dispatch: (action: PolicyAction) => void;
  readonly canEdit: boolean;
}

function PolicyRow({ row, roles, renderTrees, dispatch, canEdit }: PolicyRowProps) {
  const { node, depth } = row;
  const isGroup = node.kind === "group";
  const targetPermissions = isGroup ? node.descendantPermissions : [node.permission];
  // 1.25rem of indent per depth level so the tree shape reads at a glance
  // without resorting to chevrons or border-left tricks.
  const indent = `${depth * 1.25}rem`;

  return (
    <tr className={isGroup ? "bg-muted/30" : undefined}>
      <th scope="row" className="px-4 py-2 text-left align-top font-normal">
        <div style={{ paddingLeft: indent }}>
          <div className={isGroup ? "font-semibold" : "font-medium"}>{node.displayName}</div>
          {!isGroup ? (
            <div className="break-all text-xs text-muted-foreground">
              <code>{node.permission}</code>
              {node.operations.length > 1 ? ` · grants ${node.operations.length} operations` : null}
            </div>
          ) : null}
        </div>
      </th>
      {roles.map((role, roleIndex) => {
        const renderTree = renderTrees[roleIndex];
        const renderNode = renderTree ? findRenderNode(renderTree, node.path) : undefined;
        const cellState: NodeState = renderNode?.state ?? "off";
        return (
          <td key={role.roleKey} className="px-4 py-2 align-top">
            <TriStateCheckbox
              state={cellState}
              disabled={!canEdit}
              ariaLabel={`${role.displayName}: ${node.displayName}`}
              onChange={(checked) =>
                dispatch({
                  type: "set",
                  roleIndex,
                  permissions: targetPermissions,
                  checked,
                })
              }
            />
          </td>
        );
      })}
    </tr>
  );
}

interface TriStateCheckboxProps {
  readonly state: NodeState;
  readonly disabled?: boolean;
  readonly ariaLabel: string;
  readonly onChange: (checked: boolean) => void;
}

function TriStateCheckbox({ state, disabled, ariaLabel, onChange }: TriStateCheckboxProps) {
  const ref = useRef<HTMLInputElement>(null);
  // HTMLInputElement.indeterminate is a JS-only property; React does not
  // surface it as a JSX prop, so we set it imperatively after every render.
  // The DOM transition on click (mixed → checked) lines up with the
  // intent-driven dispatch below, so we never read state inside onChange.
  useEffect(() => {
    if (ref.current !== null) {
      ref.current.indeterminate = state === "mixed";
    }
  }, [state]);
  return (
    <input
      ref={ref}
      type="checkbox"
      checked={state === "on"}
      disabled={disabled}
      aria-label={ariaLabel}
      aria-checked={state === "mixed" ? "mixed" : state === "on"}
      onChange={(event) => onChange(event.target.checked)}
      className="h-4 w-4 cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
    />
  );
}
