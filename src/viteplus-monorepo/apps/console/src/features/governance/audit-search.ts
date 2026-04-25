import * as v from "valibot";
import type { AuditEventsQuery } from "~/lib/governance-api";

// Column IDs double as URL tokens and render keys — keep them short, stable,
// and lowercase. Adding a new column means extending this list and the
// renderer in components.tsx. Unknown IDs arriving from a stale URL are
// filtered out silently by vColumns.
export const AUDIT_COLUMN_IDS = [
  "time",
  "id",
  "risk",
  "actor",
  "operation",
  "target",
  "result",
  "area",
  "location",
  "service",
  "sequence",
  "trace",
  "credential",
  "decision",
  "event",
] as const;
export type AuditColumnId = (typeof AUDIT_COLUMN_IDS)[number];

// Default columns deliberately omit sequence/service/trace/credential/
// decision/event — the investigator picks those up from the row when needed,
// and the URL stays clean until a user deviates from the default shape.
export const DEFAULT_AUDIT_COLUMNS: ReadonlyArray<AuditColumnId> = [
  "time",
  "id",
  "risk",
  "actor",
  "operation",
  "target",
  "result",
  "area",
  "location",
];

export const DEFAULT_AUDIT_LIMIT = 50;
export const DEFAULT_AUDIT_ORDER: AuditOrder = "desc";
export const DEFAULT_AUDIT_VIEW: AuditView = "high-risk";

const operationTypes = [
  "read",
  "write",
  "delete",
  "authn",
  "authz",
  "billing",
  "export",
  "system",
  "unknown",
] as const;
export type OperationType = (typeof operationTypes)[number];

const results = ["allowed", "denied", "error"] as const;
export type AuditResult = (typeof results)[number];

const risks = ["low", "medium", "high", "critical"] as const;
export type RiskLevel = (typeof risks)[number];

export const AUDIT_LIMIT_CHOICES = [25, 50, 100, 200] as const;
export type AuditLimit = (typeof AUDIT_LIMIT_CHOICES)[number];

const orders = ["asc", "desc"] as const;
export type AuditOrder = (typeof orders)[number];

const views = ["high-risk", "all"] as const;
export type AuditView = (typeof views)[number];

// TanStack Router's default search parser runs JSON.parse on each URL value
// (src: @tanstack/router-core/searchParams). So:
//   ?limit=50 arrives as number 50
//   ?cols=["a","b"] arrives as array ["a","b"]
//   ?cols=a,b arrives as string "a,b" (JSON.parse fails, falls back to raw)
// vLimit and vColumns each accept both the native type (from JSON.parse) and
// the string fallback (a human typed the URL) so every shareable URL shape
// works.

const vLimit = v.union([
  v.literal(25),
  v.literal(50),
  v.literal(100),
  v.literal(200),
  v.pipe(
    v.literal("25"),
    v.transform(() => 25 as AuditLimit),
  ),
  v.pipe(
    v.literal("50"),
    v.transform(() => 50 as AuditLimit),
  ),
  v.pipe(
    v.literal("100"),
    v.transform(() => 100 as AuditLimit),
  ),
  v.pipe(
    v.literal("200"),
    v.transform(() => 200 as AuditLimit),
  ),
]);

const vColumns = v.pipe(
  v.union([
    v.pipe(
      v.string(),
      v.transform((raw) => raw.split(",")),
    ),
    v.array(v.string()),
  ]),
  v.transform((raw) =>
    raw
      .map((s) => s.trim())
      .filter((s): s is AuditColumnId => (AUDIT_COLUMN_IDS as readonly string[]).includes(s)),
  ),
);

const vFilterText = v.pipe(v.string(), v.minLength(1), v.maxLength(200));

export const vAuditSearch = v.object({
  view: v.optional(v.picklist(views)),
  cursor: v.optional(v.pipe(v.string(), v.maxLength(64))),
  limit: v.optional(vLimit),
  order: v.optional(v.picklist(orders)),
  risk_level: v.optional(v.picklist(risks)),
  result: v.optional(v.picklist(results)),
  operation_type: v.optional(v.picklist(operationTypes)),
  source_product_area: v.optional(vFilterText),
  service_name: v.optional(vFilterText),
  actor_id: v.optional(vFilterText),
  target_id: v.optional(vFilterText),
  target_kind: v.optional(vFilterText),
  operation_id: v.optional(vFilterText),
  audit_event: v.optional(vFilterText),
  credential_id: v.optional(vFilterText),
  cols: v.optional(vColumns),
});

export type AuditSearch = v.InferOutput<typeof vAuditSearch>;

export function parseAuditSearch(search: Record<string, unknown>): AuditSearch {
  // safeParse fallback so a malformed URL renders the default view instead
  // of crashing — shareable audit-trail links end up in tickets and Slack
  // threads where a schema drift shouldn't produce a broken page.
  const result = v.safeParse(vAuditSearch, search);
  return result.success ? result.output : {};
}

// Projection into the governance-api client's query shape. Cols/view are
// UI-local concerns — view decomposes into the server's high_risk flag only
// when no explicit filter is acting, so toggling "All activity" means the URL
// carries neither `view` nor `high_risk`.
export function auditSearchToQuery(search: AuditSearch): AuditEventsQuery {
  const query: AuditEventsQuery = {};
  if (search.cursor !== undefined) query.cursor = search.cursor;
  if (search.limit !== undefined) query.limit = search.limit;
  if (search.order !== undefined) query.order = search.order;
  if (search.risk_level !== undefined) query.risk_level = search.risk_level;
  if (search.result !== undefined) query.result = search.result;
  if (search.operation_type !== undefined) query.operation_type = search.operation_type;
  if (search.source_product_area !== undefined)
    query.source_product_area = search.source_product_area;
  if (search.service_name !== undefined) query.service_name = search.service_name;
  if (search.actor_id !== undefined) query.actor_id = search.actor_id;
  if (search.target_id !== undefined) query.target_id = search.target_id;
  if (search.target_kind !== undefined) query.target_kind = search.target_kind;
  if (search.operation_id !== undefined) query.operation_id = search.operation_id;
  if (search.audit_event !== undefined) query.audit_event = search.audit_event;
  if (search.credential_id !== undefined) query.credential_id = search.credential_id;
  if (resolveView(search) === "high-risk") query.high_risk = true;
  return query;
}

// The "high-risk" view is the default landing. It is also the inferred view
// when no explicit filter is active AND the URL hasn't said `view=all`. As
// soon as the user applies any server-facing filter, the preset collapses to
// "all" — the filter is now doing the narrowing work and stacking the
// high-risk predicate on top would silently hide their own selection.
export function resolveView(search: AuditSearch): AuditView {
  if (search.view === "all") return "all";
  if (search.view === "high-risk") return "high-risk";
  if (activeFilters(search).length > 0) return "all";
  return DEFAULT_AUDIT_VIEW;
}

export const AUDIT_FILTER_KEYS = [
  "risk_level",
  "result",
  "operation_type",
  "audit_event",
  "actor_id",
  "target_kind",
  "target_id",
  "credential_id",
  "source_product_area",
  "service_name",
  "operation_id",
] as const;
export type AuditFilterKey = (typeof AUDIT_FILTER_KEYS)[number];

export interface FilterOption {
  readonly value: string;
  readonly label: string;
}

export interface FilterDefinition {
  readonly key: AuditFilterKey;
  readonly label: string;
  readonly help: string;
  readonly kind: "enum" | "text";
  readonly group: "what" | "who" | "where";
  readonly options?: ReadonlyArray<FilterOption>;
}

// Order here drives the Add-filter popover. Keep investigator priority:
// Risk → Result → Operation type → Event name (what), Actor → Target →
// Credential (who/what), Area → Service → Operation ID (where).
export const AUDIT_FILTER_DEFINITIONS: Readonly<Record<AuditFilterKey, FilterDefinition>> = {
  risk_level: {
    key: "risk_level",
    label: "Risk",
    help: "Policy-assigned severity.",
    kind: "enum",
    group: "what",
    options: risks.map((value) => ({ value, label: value })),
  },
  result: {
    key: "result",
    label: "Result",
    help: "Outcome reported by the enforcement boundary.",
    kind: "enum",
    group: "what",
    options: results.map((value) => ({ value, label: value })),
  },
  operation_type: {
    key: "operation_type",
    label: "Operation type",
    help: "Catalog-declared operation class.",
    kind: "enum",
    group: "what",
    options: operationTypes.map((value) => ({ value, label: value })),
  },
  audit_event: {
    key: "audit_event",
    label: "Event name",
    help: "Stable audit event name.",
    kind: "text",
    group: "what",
  },
  actor_id: {
    key: "actor_id",
    label: "Actor ID",
    help: "Exact authenticated actor ID.",
    kind: "text",
    group: "who",
  },
  target_kind: {
    key: "target_kind",
    label: "Target kind",
    help: "Resource kind declared by the operation catalog.",
    kind: "text",
    group: "who",
  },
  target_id: {
    key: "target_id",
    label: "Target ID",
    help: "Resource identifier when exposed.",
    kind: "text",
    group: "who",
  },
  credential_id: {
    key: "credential_id",
    label: "API credential",
    help: "API credential ID when the actor is a credential.",
    kind: "text",
    group: "who",
  },
  source_product_area: {
    key: "source_product_area",
    label: "Area",
    help: "Customer-facing area that produced the event.",
    kind: "text",
    group: "where",
  },
  service_name: {
    key: "service_name",
    label: "Service",
    help: "Internal service that enforced the operation.",
    kind: "text",
    group: "where",
  },
  operation_id: {
    key: "operation_id",
    label: "Operation ID",
    help: "OpenAPI operation ID enforced.",
    kind: "text",
    group: "where",
  },
};

export const AUDIT_FILTER_GROUPS: ReadonlyArray<{
  readonly id: FilterDefinition["group"];
  readonly label: string;
}> = [
  { id: "what", label: "What happened" },
  { id: "who", label: "Who / what" },
  { id: "where", label: "Where" },
];

export function resolveVisibleColumns(search: AuditSearch): ReadonlyArray<AuditColumnId> {
  if (search.cols && search.cols.length > 0) return search.cols;
  return DEFAULT_AUDIT_COLUMNS;
}

export function activeFilters(search: AuditSearch): ReadonlyArray<AuditFilterKey> {
  return AUDIT_FILTER_KEYS.filter((key) => search[key] !== undefined);
}

// Returns true when the two column arrays describe the same column set in the
// same canonical order. Used by the route's stripSearchParams middleware to
// drop `cols` from the URL when it equals the default view.
export function isDefaultAuditColumns(cols: ReadonlyArray<AuditColumnId> | undefined): boolean {
  if (cols === undefined) return true;
  if (cols.length !== DEFAULT_AUDIT_COLUMNS.length) return false;
  for (let index = 0; index < cols.length; index += 1) {
    if (cols[index] !== DEFAULT_AUDIT_COLUMNS[index]) return false;
  }
  return true;
}
