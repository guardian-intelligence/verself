import * as v from "valibot";
import type { AuditEventsQuery } from "~/lib/governance-api";

// Column IDs double as URL tokens and render keys — keep them short, stable,
// and lowercase. Adding a new column means extending this list and the
// renderer in components.tsx. Unknown IDs arriving from a stale URL are
// filtered out silently by vColumns.
export const AUDIT_COLUMN_IDS = [
  "time",
  "risk",
  "actor",
  "operation",
  "target",
  "result",
  "location",
  "source",
  "sequence",
  "trace",
  "credential",
  "decision",
  "event",
] as const;
export type AuditColumnId = (typeof AUDIT_COLUMN_IDS)[number];

export const DEFAULT_AUDIT_COLUMNS: ReadonlyArray<AuditColumnId> = [
  "time",
  "risk",
  "actor",
  "operation",
  "target",
  "result",
  "location",
  "source",
  "sequence",
];

export const DEFAULT_AUDIT_LIMIT = 50;

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

// TanStack Router's default search parser runs JSON.parse on each URL value
// (src: @tanstack/router-core/searchParams). So:
//   ?limit=50 arrives as number 50
//   ?high_risk=true arrives as boolean true
//   ?cols=["a","b"] arrives as array ["a","b"]
//   ?cols=a,b arrives as string "a,b" (JSON.parse fails, falls back to raw)
// vBoolFlag, vLimit, and vColumns each accept both the native type (from
// JSON.parse) and the string fallback (a human typed the URL) so every
// shareable URL shape works.

const vBoolFlag = v.union([
  v.literal(true),
  v.literal(false),
  v.pipe(
    v.literal("true"),
    v.transform(() => true),
  ),
  v.pipe(
    v.literal("false"),
    v.transform(() => false),
  ),
]);

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
  cursor: v.optional(v.pipe(v.string(), v.maxLength(4096))),
  limit: v.optional(vLimit),
  high_risk: v.optional(vBoolFlag),
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

// Projection into the governance-api client's query shape. `cols` is a
// UI-local concern and never flows to the server.
export function auditSearchToQuery(search: AuditSearch): AuditEventsQuery {
  const query: AuditEventsQuery = {};
  if (search.cursor !== undefined) query.cursor = search.cursor;
  if (search.limit !== undefined) query.limit = search.limit;
  if (search.high_risk !== undefined) query.high_risk = search.high_risk;
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
  return query;
}

export const AUDIT_FILTER_KEYS = [
  "high_risk",
  "risk_level",
  "result",
  "operation_type",
  "source_product_area",
  "service_name",
  "target_kind",
  "actor_id",
  "target_id",
  "operation_id",
  "audit_event",
  "credential_id",
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
  readonly kind: "boolean" | "enum" | "text";
  readonly options?: ReadonlyArray<FilterOption>;
}

export const AUDIT_FILTER_DEFINITIONS: Readonly<Record<AuditFilterKey, FilterDefinition>> = {
  high_risk: {
    key: "high_risk",
    label: "High-risk only",
    help: "Writes, deletes, exports, denials, and errors — the operator-interest subset.",
    kind: "boolean",
  },
  risk_level: {
    key: "risk_level",
    label: "Risk",
    help: "Policy-assigned severity.",
    kind: "enum",
    options: risks.map((value) => ({ value, label: value })),
  },
  result: {
    key: "result",
    label: "Result",
    help: "Outcome reported by the enforcement boundary.",
    kind: "enum",
    options: results.map((value) => ({ value, label: value })),
  },
  operation_type: {
    key: "operation_type",
    label: "Operation type",
    help: "Catalog-declared operation class.",
    kind: "enum",
    options: operationTypes.map((value) => ({ value, label: value })),
  },
  source_product_area: {
    key: "source_product_area",
    label: "Product area",
    help: "Customer-facing area that produced the event.",
    kind: "text",
  },
  service_name: {
    key: "service_name",
    label: "Service",
    help: "Internal service that enforced the operation.",
    kind: "text",
  },
  target_kind: {
    key: "target_kind",
    label: "Target kind",
    help: "Resource kind declared by the operation catalog.",
    kind: "text",
  },
  actor_id: {
    key: "actor_id",
    label: "Actor ID",
    help: "Exact authenticated actor ID.",
    kind: "text",
  },
  target_id: {
    key: "target_id",
    label: "Target ID",
    help: "Resource identifier when exposed.",
    kind: "text",
  },
  operation_id: {
    key: "operation_id",
    label: "Operation ID",
    help: "OpenAPI operation ID enforced.",
    kind: "text",
  },
  audit_event: {
    key: "audit_event",
    label: "Event name",
    help: "Stable audit event name.",
    kind: "text",
  },
  credential_id: {
    key: "credential_id",
    label: "API credential",
    help: "API credential ID when the actor is a credential.",
    kind: "text",
  },
};

export function resolveVisibleColumns(search: AuditSearch): ReadonlyArray<AuditColumnId> {
  if (search.cols && search.cols.length > 0) return search.cols;
  return DEFAULT_AUDIT_COLUMNS;
}

export function activeFilters(search: AuditSearch): ReadonlyArray<AuditFilterKey> {
  return AUDIT_FILTER_KEYS.filter((key) => search[key] !== undefined);
}
