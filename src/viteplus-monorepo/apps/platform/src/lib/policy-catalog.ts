// Policy catalog — parses the machine-readable policy source at module load
// and exposes typed constants every /policy/* page renders against. Raw YAML
// files live in src/__generated/policies/, refreshed from the canonical
// src/platform/policies/ tree by the `generate` package script. Keeping both
// sides schema-gated on parse means a malformed edit breaks the build, not
// just the runtime page.
import * as v from "valibot";
import { parse as parseYaml } from "yaml";

import retentionYaml from "../__generated/policies/retention.yml?raw";
import subprocessorsYaml from "../__generated/policies/subprocessors.yml?raw";
import ropaYaml from "../__generated/policies/ropa.yml?raw";
import contactsYaml from "../__generated/policies/contacts.yml?raw";
import versionsYaml from "../__generated/policies/versions.yml?raw";

// ─── retention ──────────────────────────────────────────────────────────────

const LifecycleKeySchema = v.picklist([
  "active",
  "past_due",
  "suspended",
  "pending_deletion",
  "deleted",
]);
export type LifecycleKey = v.InferOutput<typeof LifecycleKeySchema>;

const NonTerminalLifecycleKeySchema = v.picklist([
  "active",
  "past_due",
  "suspended",
  "pending_deletion",
]);
export type NonTerminalLifecycleKey = v.InferOutput<typeof NonTerminalLifecycleKeySchema>;

const StateSchema = v.object({
  key: LifecycleKeySchema,
  label: v.string(),
  blurb: v.string(),
});

const TransitionSchema = v.object({
  from: LifecycleKeySchema,
  to: LifecycleKeySchema,
  trigger: v.string(),
  days: v.optional(v.number()),
});

// Every data class's per-state retention behavior is one of a fixed set of
// shapes. Modeling them as a discriminated union rather than free-form strings
// lets the renderer exhaustively switch on `kind`.
const WindowValueSchema = v.variant("kind", [
  v.object({ kind: v.literal("preserved") }),
  v.object({ kind: v.literal("per_user_policy") }),
  v.object({ kind: v.literal("delete_with_parent") }),
  v.object({ kind: v.literal("not_provided") }),
  v.object({ kind: v.literal("delete_after"), days: v.number() }),
  v.object({ kind: v.literal("ttl_days"), days: v.number() }),
  v.object({ kind: v.literal("retain_years"), years: v.number() }),
]);
export type WindowValue = v.InferOutput<typeof WindowValueSchema>;

const WindowSchema = v.object({
  id: v.string(),
  label: v.string(),
  description: v.string(),
  source: v.optional(v.string()),
  active: WindowValueSchema,
  past_due: WindowValueSchema,
  suspended: WindowValueSchema,
  pending_deletion: WindowValueSchema,
});
export type Window = v.InferOutput<typeof WindowSchema>;

const ExportFormatSchema = v.object({
  class: v.string(),
  format: v.string(),
});

const RetentionSchema = v.object({
  version: v.number(),
  effective_at: v.string(),
  state_machine: v.array(StateSchema),
  transitions: v.array(TransitionSchema),
  windows: v.array(WindowSchema),
  export: v.object({
    available_during: v.array(NonTerminalLifecycleKeySchema),
    post_closure_days: v.number(),
    resets_clock: v.boolean(),
    delivery: v.string(),
    formats: v.array(ExportFormatSchema),
  }),
  extensions: v.object({
    allow_multiple: v.boolean(),
    clock_behavior: v.string(),
    decline_conditions: v.string(),
    audited_fields: v.array(v.string()),
  }),
  legal_hold: v.object({
    behavior: v.string(),
  }),
  deletion: v.object({
    soft_delete: v.boolean(),
    recoverable_after_execution: v.boolean(),
    methods: v.array(v.string()),
  }),
  anonymized_data: v.object({
    retained_indefinitely: v.boolean(),
    description: v.string(),
  }),
  changes: v.object({
    notice_days: v.number(),
    notification_channel: v.string(),
    prior_versions_surface: v.string(),
  }),
});
export type Retention = v.InferOutput<typeof RetentionSchema>;

// ─── subprocessors ──────────────────────────────────────────────────────────

const SubprocessorSchema = v.object({
  id: v.string(),
  name: v.string(),
  purpose: v.string(),
  data_categories: v.array(v.string()),
  processing_location: v.string(),
  dpa_url: v.pipe(v.string(), v.url()),
});
export type Subprocessor = v.InferOutput<typeof SubprocessorSchema>;

const SubprocessorsSchema = v.object({
  version: v.number(),
  effective_at: v.string(),
  subprocessors: v.array(SubprocessorSchema),
  change_notification: v.object({
    channel: v.string(),
    lead_time_days: v.number(),
  }),
});
export type Subprocessors = v.InferOutput<typeof SubprocessorsSchema>;

// ─── record of processing activities ────────────────────────────────────────

const ProcessingActivitySchema = v.object({
  id: v.string(),
  role: v.picklist(["controller", "processor"]),
  purpose: v.string(),
  data_categories: v.array(v.string()),
  lawful_basis: v.string(),
  retention_ref: v.string(),
});
export type ProcessingActivity = v.InferOutput<typeof ProcessingActivitySchema>;

const RopaSchema = v.object({
  version: v.number(),
  effective_at: v.string(),
  processing_activities: v.array(ProcessingActivitySchema),
});
export type Ropa = v.InferOutput<typeof RopaSchema>;

// ─── contacts ───────────────────────────────────────────────────────────────

const ContactsSchema = v.object({
  version: v.number(),
  effective_at: v.string(),
  mailboxes: v.object({
    policy: v.string(),
    privacy: v.string(),
    security: v.string(),
    dpo: v.string(),
    abuse: v.string(),
    legal: v.string(),
  }),
  routing: v.string(),
});
export type Contacts = v.InferOutput<typeof ContactsSchema>;

// ─── versions / changelog ───────────────────────────────────────────────────

const PolicyIdSchema = v.picklist([
  "terms",
  "privacy",
  "acceptable-use",
  "dpa",
  "subprocessors",
  "security",
  "sla",
  "cookies",
  "data-retention",
]);
export type PolicyId = v.InferOutput<typeof PolicyIdSchema>;

const VersionEntrySchema = v.object({
  date: v.string(),
  version: v.string(),
  policies: v.array(PolicyIdSchema),
  summary: v.string(),
});
export type VersionEntry = v.InferOutput<typeof VersionEntrySchema>;

const VersionsSchema = v.object({
  entries: v.array(VersionEntrySchema),
});
export type Versions = v.InferOutput<typeof VersionsSchema>;

// ─── parse once at module load ──────────────────────────────────────────────

function parseStrict<TSchema extends v.GenericSchema>(
  schema: TSchema,
  raw: string,
  label: string,
): v.InferOutput<TSchema> {
  const parsed = parseYaml(raw);
  const result = v.safeParse(schema, parsed);
  if (!result.success) {
    throw new Error(
      `policy source ${label} failed validation: ` +
        result.issues
          .map((i) => `${i.path?.map((p) => p.key).join(".") ?? "<root>"}: ${i.message}`)
          .join("; "),
    );
  }
  return result.output as v.InferOutput<TSchema>;
}

export const RETENTION: Retention = parseStrict(RetentionSchema, retentionYaml, "retention.yml");
export const SUBPROCESSORS: Subprocessors = parseStrict(
  SubprocessorsSchema,
  subprocessorsYaml,
  "subprocessors.yml",
);
export const ROPA: Ropa = parseStrict(RopaSchema, ropaYaml, "ropa.yml");
export const CONTACTS: Contacts = parseStrict(ContactsSchema, contactsYaml, "contacts.yml");
export const VERSIONS: Versions = parseStrict(VersionsSchema, versionsYaml, "versions.yml");

// ─── derived helpers ────────────────────────────────────────────────────────

export type Mailboxes = {
  readonly policy: string;
  readonly privacy: string;
  readonly security: string;
  readonly dpo: string;
  readonly abuse: string;
  readonly legal: string;
};

export function deriveMailboxes(operatorDomain: string): Mailboxes {
  const { mailboxes } = CONTACTS;
  return {
    policy: `${mailboxes.policy}@${operatorDomain}`,
    privacy: `${mailboxes.privacy}@${operatorDomain}`,
    security: `${mailboxes.security}@${operatorDomain}`,
    dpo: `${mailboxes.dpo}@${operatorDomain}`,
    abuse: `${mailboxes.abuse}@${operatorDomain}`,
    legal: `${mailboxes.legal}@${operatorDomain}`,
  };
}

export function findWindow(id: string): Window | undefined {
  return RETENTION.windows.find((w) => w.id === id);
}

export function latestVersion(): VersionEntry {
  const sorted = [...VERSIONS.entries].sort((a, b) => (a.date < b.date ? 1 : -1));
  const first = sorted[0];
  if (!first) {
    throw new Error("versions.yml: entries is empty");
  }
  return first;
}

export function effectiveDateOf(policy: PolicyId): string {
  // The effective date of a policy is the date of the most recent versions.yml
  // entry that listed that policy. Keeps every page's header honest without
  // a separate effective_at on every YAML file.
  const sorted = [...VERSIONS.entries].sort((a, b) => (a.date < b.date ? 1 : -1));
  for (const entry of sorted) {
    if (entry.policies.includes(policy)) return entry.date;
  }
  throw new Error(`versions.yml: no entry lists policy "${policy}"`);
}

// Human-readable rendering for a WindowValue cell. Rendering stays next to the
// schema so every page that shows a retention window goes through one
// well-named helper.
export function formatWindowValue(value: WindowValue): string {
  switch (value.kind) {
    case "preserved":
      return "Preserved";
    case "per_user_policy":
      return "Per your configured retention policy";
    case "delete_with_parent":
      return "Deleted with the parent volume";
    case "not_provided":
      return "Not provided";
    case "delete_after":
      return `Deleted at ${value.days} days`;
    case "ttl_days":
      return `${value.days}-day TTL`;
    case "retain_years":
      return `Retained for ${value.years} years`;
  }
}

export function formatPrettyDate(isoDate: string): string {
  const d = new Date(`${isoDate}T00:00:00Z`);
  return d.toLocaleDateString("en-US", {
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}
