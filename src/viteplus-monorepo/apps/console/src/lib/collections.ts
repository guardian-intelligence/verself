import * as v from "valibot";
import { authCollectionId, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import {
  electricAndWhere,
  createElectricShapeCollection,
  electricEqualsWhere,
  electricStringifiedBooleanSchema,
  electricStringifiedIntegerSchema,
  requireDecimalID,
  requireElectricOpaqueID,
  requireUUID,
} from "@verself/web-env";

// Module-level cache of live collections keyed by their stable `id`. Route
// components call the factories below from a `useMemo`, which is per-mount
// state: without this cache, navigating to an execution detail page and
// back remounts the list route, restarts the useMemo, and produces a
// *brand new* Electric shape subscription whose rows show up only after a
// round-trip. Callers see `isIdle → isLoading → loaded` on every back nav.
// Hoisting the instance here makes the collection durable across mounts,
// which is what Electric shapes are designed for (the `id` field is the
// subscription identity the server tracks). Every factory below validates
// its scope inputs *before* consulting the cache so a bad org/attempt id
// still throws instead of silently returning a stale instance.
//
// SSR safety: consumers of these factories are gated behind `<ClientOnly>`,
// so this map is only populated in the browser. The server render never
// reaches the factories and the module's top-level state stays empty.
const electricCollectionCache = new Map<string, unknown>();

function cachedElectricCollection<T>(id: string, factory: () => T): T {
  const existing = electricCollectionCache.get(id);
  if (existing !== undefined) return existing as T;
  const collection = factory();
  electricCollectionCache.set(id, collection);
  return collection;
}

// --- Execution collection (real-time PG sync via Electric) ---

const electricExecutionRowSchema = v.object({
  execution_id: v.string(),
  org_id: v.string(),
  actor_id: v.string(),
  kind: v.string(),
  source_kind: v.string(),
  workload_kind: v.string(),
  source_ref: v.string(),
  runner_class: v.string(),
  external_provider: v.string(),
  external_task_id: v.string(),
  provider: v.string(),
  product_id: v.string(),
  state: v.string(),
  correlation_id: v.string(),
  idempotency_key: v.string(),
  run_command: v.string(),
  max_wall_seconds: electricStringifiedIntegerSchema,
  created_at: v.string(),
  updated_at: v.string(),
});

// TanStack DB does not apply Valibot transforms to synced data — the schema
// is used for type inference and client-side mutation validation only.  The
// previous v.transform that mapped state→status never ran, so the UI received
// the raw Electric row where `status` was undefined.  Use the row schema
// directly and read `state` in components.
export type ElectricExecution = v.InferOutput<typeof electricExecutionRowSchema>;

export function createExecutionsCollection(auth: AuthenticatedAuth, orgId: string) {
  const validatedOrgID = requireDecimalID(orgId, "org_id");
  const id = authCollectionId(auth, `sync-executions-${orgId}`);
  return cachedElectricCollection(id, () =>
    createElectricShapeCollection({
      id,
      schema: electricExecutionRowSchema,
      table: "executions",
      where: electricEqualsWhere("org_id", validatedOrgID),
      getKey: (item) => item.execution_id,
    }),
  );
}

// --- Runner repository bindings (execution source_ref -> source repo id) ---

const electricRunnerProviderRepositorySchema = v.object({
  provider: v.string(),
  provider_repository_id: electricStringifiedIntegerSchema,
  org_id: v.string(),
  project_id: v.nullable(v.string()),
  source_repository_id: v.nullable(v.string()),
  provider_owner: v.string(),
  provider_repo: v.string(),
  repository_full_name: v.string(),
  active: electricStringifiedBooleanSchema,
  created_at: v.string(),
  updated_at: v.string(),
});

export type ElectricRunnerProviderRepository = v.InferOutput<
  typeof electricRunnerProviderRepositorySchema
>;

export function createRunnerProviderRepositoriesCollection(auth: AuthenticatedAuth, orgId: string) {
  const validatedOrgID = requireDecimalID(orgId, "org_id");
  const id = authCollectionId(auth, `sync-runner-provider-repositories-${orgId}`);
  return cachedElectricCollection(id, () =>
    createElectricShapeCollection({
      id,
      schema: electricRunnerProviderRepositorySchema,
      table: "runner_provider_repositories",
      where: electricEqualsWhere("org_id", validatedOrgID),
      getKey: (item) => `${item.provider}:${item.provider_repository_id}`,
    }),
  );
}

// --- Execution log chunks (real-time streaming via Electric) ---

const electricExecutionLogSchema = v.object({
  attempt_id: v.string(),
  seq: electricStringifiedIntegerSchema,
  stream: v.string(),
  chunk: v.string(),
  created_at: v.string(),
});

export type ElectricExecutionLog = v.InferOutput<typeof electricExecutionLogSchema>;

export function createExecutionLogsCollection(auth: AuthenticatedAuth, attemptId: string) {
  const validatedAttemptID = requireUUID(attemptId, "attempt_id");
  const id = authCollectionId(auth, `sync-execution-logs-${attemptId}`);
  return cachedElectricCollection(id, () =>
    createElectricShapeCollection({
      id,
      schema: electricExecutionLogSchema,
      table: "execution_logs",
      where: electricEqualsWhere("attempt_id", validatedAttemptID),
      getKey: (item) => `${item.attempt_id}:${item.seq}`,
    }),
  );
}

// --- Notification inbox state (real-time badge signal via Electric) ---

const electricNotificationInboxStateSchema = v.object({
  org_id: v.string(),
  recipient_subject_id: v.string(),
  next_sequence: electricStringifiedIntegerSchema,
  read_up_to_sequence: electricStringifiedIntegerSchema,
  created_at: v.string(),
  updated_at: v.string(),
});

export type ElectricNotificationInboxState = v.InferOutput<
  typeof electricNotificationInboxStateSchema
>;

export function createNotificationInboxStateCollection(
  auth: AuthenticatedAuth,
  orgId: string,
  subjectId: string,
) {
  const validatedOrgID = requireElectricOpaqueID(orgId, "org_id");
  const validatedSubjectID = requireElectricOpaqueID(subjectId, "recipient_subject_id");
  const id = authCollectionId(auth, `sync-notification-inbox-state-${orgId}-${subjectId}`);
  return cachedElectricCollection(id, () =>
    createElectricShapeCollection({
      id,
      schema: electricNotificationInboxStateSchema,
      table: "notification_inbox_state",
      where: electricAndWhere([
        { column: "org_id", value: validatedOrgID },
        { column: "recipient_subject_id", value: validatedSubjectID },
      ]),
      shapePath: "/notifications/v1/shape",
      getKey: (item) => `${item.org_id}:${item.recipient_subject_id}`,
    }),
  );
}
