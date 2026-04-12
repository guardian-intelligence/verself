import { useMemo, useState } from "react";
import { ChevronRightIcon } from "lucide-react";
import { Checkbox } from "@forge-metal/ui/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { cn } from "@forge-metal/ui/lib/utils";
import type { CatalogNode, GroupNode } from "./catalog.ts";
import { deriveTree, findRenderNode, type NodeState } from "./derive.ts";
import type { PolicyAction, PolicyFormRole, PolicyFormState } from "./reducer.ts";

export interface PolicyMatrixProps {
  readonly catalog: GroupNode;
  readonly state: PolicyFormState;
  readonly dispatch: (action: PolicyAction) => void;
  readonly canEdit: boolean;
}

const ROOT_KEY = "__root__";

function pathKey(node: CatalogNode): string {
  return node.path.length === 0 ? ROOT_KEY : node.path.join(":");
}

function collectGroupKeys(root: GroupNode): readonly string[] {
  const out: string[] = [];
  const walk = (node: CatalogNode): void => {
    if (node.kind !== "group") return;
    out.push(pathKey(node));
    for (const child of node.children) walk(child);
  };
  walk(root);
  return out;
}

interface FlatRow {
  readonly node: CatalogNode;
  readonly depth: number;
}

function flatten(root: GroupNode, expanded: ReadonlySet<string>): readonly FlatRow[] {
  const out: FlatRow[] = [];
  const walk = (node: CatalogNode, depth: number) => {
    out.push({ node, depth });
    if (node.kind === "group" && expanded.has(pathKey(node))) {
      for (const child of node.children) walk(child, depth + 1);
    }
  };
  for (const child of root.children) walk(child, 0);
  return out;
}

export function PolicyMatrix({ catalog, state, dispatch, canEdit }: PolicyMatrixProps) {
  // Default-expanded: every group is open. The reducer/derive code is
  // independent of this state, so collapsing only changes which rows are
  // *rendered*, never how toggles dispatch.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(collectGroupKeys(catalog)),
  );

  const renderTrees = useMemo(
    () => state.roles.map((role) => deriveTree(catalog, role.permissions)),
    [catalog, state.roles],
  );
  const rows = useMemo(() => flatten(catalog, expanded), [catalog, expanded]);

  const toggleExpanded = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  if (rows.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border px-4 py-6 text-sm text-muted-foreground">
        No service operations have been published yet.
      </div>
    );
  }

  // Table renders its own overflow-x-auto container — no outer wrapper needed.
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead scope="col">Permission</TableHead>
          {state.roles.map((role) => (
            <TableHead key={role.roleKey} scope="col">
              {role.displayName}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <PolicyRow
            key={pathKey(row.node)}
            row={row}
            roles={state.roles}
            renderTrees={renderTrees}
            dispatch={dispatch}
            canEdit={canEdit}
            isExpanded={row.node.kind === "group" ? expanded.has(pathKey(row.node)) : false}
            onToggleExpanded={() => toggleExpanded(pathKey(row.node))}
          />
        ))}
      </TableBody>
    </Table>
  );
}

interface PolicyRowProps {
  readonly row: FlatRow;
  readonly roles: readonly PolicyFormRole[];
  readonly renderTrees: ReadonlyArray<ReturnType<typeof deriveTree>>;
  readonly dispatch: (action: PolicyAction) => void;
  readonly canEdit: boolean;
  readonly isExpanded: boolean;
  readonly onToggleExpanded: () => void;
}

function PolicyRow({
  row,
  roles,
  renderTrees,
  dispatch,
  canEdit,
  isExpanded,
  onToggleExpanded,
}: PolicyRowProps) {
  const { node, depth } = row;
  const isGroup = node.kind === "group";
  const targetPermissions = isGroup ? node.descendantPermissions : [node.permission];
  // 1.25rem of indent per depth level so the tree shape reads at a glance.
  const indent = `${depth * 1.25}rem`;

  return (
    <TableRow className={isGroup ? "bg-muted/30" : undefined}>
      <TableCell className="align-top whitespace-normal">
        <div className="flex items-start gap-2" style={{ paddingLeft: indent }}>
          {isGroup ? (
            <button
              type="button"
              onClick={onToggleExpanded}
              aria-expanded={isExpanded}
              aria-label={`${isExpanded ? "Collapse" : "Expand"} ${node.displayName}`}
              className="mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <ChevronRightIcon
                className={cn("size-4 transition-transform", isExpanded && "rotate-90")}
              />
            </button>
          ) : (
            <span className="inline-block size-4 shrink-0" aria-hidden="true" />
          )}
          <div className="min-w-0">
            <div className={isGroup ? "font-semibold" : "font-medium"}>{node.displayName}</div>
            {!isGroup ? (
              <div className="break-all text-xs text-muted-foreground">
                <code>{node.permission}</code>
                {node.operations.length > 1
                  ? ` · grants ${node.operations.length} operations`
                  : null}
              </div>
            ) : null}
          </div>
        </div>
      </TableCell>
      {roles.map((role, roleIndex) => {
        const renderTree = renderTrees[roleIndex];
        const renderNode = renderTree ? findRenderNode(renderTree, node.path) : undefined;
        const cellState: NodeState = renderNode?.state ?? "off";
        return (
          <TableCell key={role.roleKey} className="align-top">
            <Checkbox
              checked={checkedFromState(cellState)}
              disabled={!canEdit}
              aria-label={`${role.displayName}: ${node.displayName}`}
              onCheckedChange={(checked: boolean | "indeterminate") =>
                dispatch({
                  type: "set",
                  roleIndex,
                  permissions: targetPermissions,
                  // Radix passes "indeterminate" as a CheckedState; treat it
                  // as "user wants on" so clicking a mixed cell fills the
                  // subtree (matches Clerk's tri-state grid behavior).
                  checked: checked === true || checked === "indeterminate",
                })
              }
            />
          </TableCell>
        );
      })}
    </TableRow>
  );
}

function checkedFromState(state: NodeState): boolean | "indeterminate" {
  if (state === "on") return true;
  if (state === "mixed") return "indeterminate";
  return false;
}
