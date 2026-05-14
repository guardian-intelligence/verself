import * as v from "valibot";
import type { GovernanceAPIActivitiesQuery } from "@verself/sdk";

export const DEFAULT_AUDIT_LIMIT = 50;
export const DEFAULT_AUDIT_ORDER: AuditOrder = "desc";
export const AUDIT_LIMIT_CHOICES = [25, 50, 100, 200] as const;
export type AuditLimit = (typeof AUDIT_LIMIT_CHOICES)[number];

const orders = ["asc", "desc"] as const;
export type AuditOrder = (typeof orders)[number];

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

const vFilterText = v.pipe(v.string(), v.minLength(1), v.maxLength(255));
const vSourceText = v.pipe(v.string(), v.minLength(1), v.maxLength(128));

export const vAuditSearch = v.object({
  activity_id: v.optional(v.pipe(v.string(), v.regex(/^[1-9][0-9]?$/))),
  actor_type: v.optional(vSourceText),
  actor_uid: v.optional(vFilterText),
  api_operation: v.optional(vSourceText),
  api_service: v.optional(vSourceText),
  credential_uid: v.optional(vFilterText),
  cursor: v.optional(v.pipe(v.string(), v.maxLength(64))),
  limit: v.optional(vLimit),
  order: v.optional(v.picklist(orders)),
  resource_type: v.optional(vSourceText),
  resource_uid: v.optional(vFilterText),
  status_code: v.optional(vFilterText),
  status_id: v.optional(v.union([v.literal("1"), v.literal("2")])),
  trace_uid: v.optional(vFilterText),
});

export type AuditSearch = v.InferOutput<typeof vAuditSearch>;

export function parseAuditSearch(search: Record<string, unknown>): AuditSearch {
  const result = v.safeParse(vAuditSearch, search);
  return result.success ? result.output : {};
}

export function auditSearchToQuery(search: AuditSearch): GovernanceAPIActivitiesQuery {
  const query: GovernanceAPIActivitiesQuery = {};
  if (search.activity_id !== undefined) query.activity_id = Number(search.activity_id);
  if (search.actor_type !== undefined) query.actor_type = search.actor_type;
  if (search.actor_uid !== undefined) query.actor_uid = search.actor_uid;
  if (search.api_operation !== undefined) query.api_operation = search.api_operation;
  if (search.api_service !== undefined) query.api_service = search.api_service;
  if (search.credential_uid !== undefined) query.credential_uid = search.credential_uid;
  if (search.cursor !== undefined) query.cursor = search.cursor;
  if (search.limit !== undefined) query.limit = search.limit;
  if (search.order !== undefined) query.order = search.order;
  if (search.resource_type !== undefined) query.resource_type = search.resource_type;
  if (search.resource_uid !== undefined) query.resource_uid = search.resource_uid;
  if (search.status_code !== undefined) query.status_code = search.status_code;
  if (search.status_id !== undefined) query.status_id = Number(search.status_id);
  if (search.trace_uid !== undefined) query.trace_uid = search.trace_uid;
  return query;
}

export const AUDIT_FILTER_KEYS = [
  "status_id",
  "api_service",
  "api_operation",
  "activity_id",
  "actor_uid",
  "resource_type",
  "resource_uid",
  "credential_uid",
  "trace_uid",
] as const;
export type AuditFilterKey = (typeof AUDIT_FILTER_KEYS)[number];

export interface FilterDefinition {
  readonly key: AuditFilterKey;
  readonly label: string;
  readonly help: string;
  readonly kind: "enum" | "text";
  readonly options?: ReadonlyArray<{ readonly value: string; readonly label: string }>;
}

export const AUDIT_FILTER_DEFINITIONS: Readonly<Record<AuditFilterKey, FilterDefinition>> = {
  status_id: {
    key: "status_id",
    label: "Status",
    help: "OCSF status_id.",
    kind: "enum",
    options: [
      { value: "1", label: "Success" },
      { value: "2", label: "Failure" },
    ],
  },
  api_service: {
    key: "api_service",
    label: "Service",
    help: "API service.",
    kind: "text",
  },
  api_operation: {
    key: "api_operation",
    label: "Operation",
    help: "API operation.",
    kind: "text",
  },
  activity_id: {
    key: "activity_id",
    label: "Activity",
    help: "OCSF activity_id.",
    kind: "text",
  },
  actor_uid: {
    key: "actor_uid",
    label: "Actor",
    help: "Actor UID.",
    kind: "text",
  },
  resource_type: {
    key: "resource_type",
    label: "Resource type",
    help: "Resource type.",
    kind: "text",
  },
  resource_uid: {
    key: "resource_uid",
    label: "Resource UID",
    help: "Resource UID.",
    kind: "text",
  },
  credential_uid: {
    key: "credential_uid",
    label: "Credential",
    help: "Credential UID.",
    kind: "text",
  },
  trace_uid: {
    key: "trace_uid",
    label: "Trace",
    help: "Trace UID.",
    kind: "text",
  },
};

export function activeFilters(search: AuditSearch): ReadonlyArray<AuditFilterKey> {
  return AUDIT_FILTER_KEYS.filter((key) => search[key] !== undefined);
}

export function withoutAuditFilters(search: AuditSearch): AuditSearch {
  const next: AuditSearch = { ...search, cursor: undefined };
  for (const key of AUDIT_FILTER_KEYS) {
    next[key] = undefined;
  }
  return next;
}
