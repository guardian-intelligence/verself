import * as v from "valibot";
import type { GovernanceAuditEventsQuery } from "@verself/sdk";

export const DEFAULT_AUDIT_LIMIT = 50;
export const DEFAULT_AUDIT_ORDER: AuditOrder = "desc";
export const AUDIT_LIMIT_CHOICES = [25, 50, 100, 200] as const;
export type AuditLimit = (typeof AUDIT_LIMIT_CHOICES)[number];

const orders = ["asc", "desc"] as const;
export type AuditOrder = (typeof orders)[number];

const outcomes = ["allowed", "denied", "error"] as const;
export type AuditOutcome = (typeof outcomes)[number];

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
  actor_id: v.optional(vFilterText),
  audit_event: v.optional(vFilterText),
  credential_id: v.optional(vFilterText),
  cursor: v.optional(v.pipe(v.string(), v.maxLength(64))),
  event_name: v.optional(vSourceText),
  event_source: v.optional(vSourceText),
  limit: v.optional(vLimit),
  order: v.optional(v.picklist(orders)),
  outcome: v.optional(v.picklist(outcomes)),
  target_id: v.optional(vFilterText),
  target_type: v.optional(vSourceText),
});

export type AuditSearch = v.InferOutput<typeof vAuditSearch>;

export function parseAuditSearch(search: Record<string, unknown>): AuditSearch {
  const result = v.safeParse(vAuditSearch, search);
  return result.success ? result.output : {};
}

export function auditSearchToQuery(search: AuditSearch): GovernanceAuditEventsQuery {
  const query: GovernanceAuditEventsQuery = {};
  if (search.actor_id !== undefined) query.actor_id = search.actor_id;
  if (search.audit_event !== undefined) query.audit_event = search.audit_event;
  if (search.credential_id !== undefined) query.credential_id = search.credential_id;
  if (search.cursor !== undefined) query.cursor = search.cursor;
  if (search.event_name !== undefined) query.event_name = search.event_name;
  if (search.event_source !== undefined) query.event_source = search.event_source;
  if (search.limit !== undefined) query.limit = search.limit;
  if (search.order !== undefined) query.order = search.order;
  if (search.outcome !== undefined) query.outcome = search.outcome;
  if (search.target_id !== undefined) query.target_id = search.target_id;
  if (search.target_type !== undefined) query.target_type = search.target_type;
  return query;
}

export const AUDIT_FILTER_KEYS = [
  "outcome",
  "event_source",
  "event_name",
  "audit_event",
  "actor_id",
  "target_type",
  "target_id",
  "credential_id",
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
  outcome: {
    key: "outcome",
    label: "Outcome",
    help: "Authorization outcome.",
    kind: "enum",
    options: outcomes.map((value) => ({ value, label: value })),
  },
  event_source: {
    key: "event_source",
    label: "Source",
    help: "Recording service or system.",
    kind: "text",
  },
  event_name: {
    key: "event_name",
    label: "Operation",
    help: "Stable operation name.",
    kind: "text",
  },
  audit_event: {
    key: "audit_event",
    label: "Audit event",
    help: "Stable audit event name.",
    kind: "text",
  },
  actor_id: {
    key: "actor_id",
    label: "Actor",
    help: "Authenticated actor ID.",
    kind: "text",
  },
  target_type: {
    key: "target_type",
    label: "Target type",
    help: "Resource type.",
    kind: "text",
  },
  target_id: {
    key: "target_id",
    label: "Target ID",
    help: "Resource identifier.",
    kind: "text",
  },
  credential_id: {
    key: "credential_id",
    label: "Credential",
    help: "Credential ID.",
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
