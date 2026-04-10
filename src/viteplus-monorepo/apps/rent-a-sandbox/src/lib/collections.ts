import * as v from "valibot";
import {
  createElectricShapeCollection,
  electricEqualsWhere,
  electricStringifiedIntegerSchema,
  requireDecimalID,
  requireUUID,
} from "@forge-metal/web-env";

// --- Execution collection (real-time PG sync via Electric) ---

const electricExecutionSchema = v.object({
  execution_id: v.string(),
  org_id: v.string(),
  actor_id: v.string(),
  kind: v.string(),
  provider: v.string(),
  product_id: v.string(),
  status: v.string(),
  correlation_id: v.string(),
  idempotency_key: v.nullable(v.string()),
  repo_id: v.nullable(v.string()),
  golden_generation_id: v.nullable(v.string()),
  repo: v.string(),
  repo_url: v.string(),
  ref: v.string(),
  default_branch: v.string(),
  run_command: v.string(),
  commit_sha: v.string(),
  workflow_path: v.string(),
  workflow_job_name: v.string(),
  provider_run_id: v.string(),
  provider_job_id: v.string(),
  latest_attempt_id: v.string(),
  created_at: v.string(),
  updated_at: v.string(),
});

export type ElectricExecution = v.InferOutput<typeof electricExecutionSchema>;

export function createExecutionsCollection(orgId: string) {
  const validatedOrgID = requireDecimalID(orgId, "org_id");
  return createElectricShapeCollection({
    id: `sync-executions-${orgId}`,
    schema: electricExecutionSchema,
    table: "executions",
    where: electricEqualsWhere("org_id", validatedOrgID),
    getKey: (item) => item.execution_id,
  });
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

export function createExecutionLogsCollection(attemptId: string) {
  const validatedAttemptID = requireUUID(attemptId, "attempt_id");
  return createElectricShapeCollection({
    id: `sync-execution-logs-${attemptId}`,
    schema: electricExecutionLogSchema,
    table: "execution_logs",
    where: electricEqualsWhere("attempt_id", validatedAttemptID),
    getKey: (item) => `${item.attempt_id}:${item.seq}`,
  });
}
