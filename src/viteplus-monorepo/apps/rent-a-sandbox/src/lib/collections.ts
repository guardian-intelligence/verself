import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

// Electric shape URL — proxied through Caddy at the same origin.
// Electric syncs the sandbox_rental database only. Billing data
// (subscriptions, grants, balance) uses TanStack Query — balance is
// TigerBeetle-derived and billing tables live in a separate PG database.
const ELECTRIC_SHAPE_URL = "/v1/shape";

// --- Execution collection (real-time PG sync via Electric) ---

export interface ElectricExecution {
  execution_id: string;
  org_id: string;
  actor_id: string;
  kind: string;
  provider: string;
  product_id: string;
  status: string;
  repo: string;
  repo_url: string;
  ref: string;
  default_branch: string;
  run_command: string;
  commit_sha: string;
  latest_attempt_id: string;
  created_at: string;
  updated_at: string;
}

export function createExecutionsCollection(orgId: string) {
  return createCollection<ElectricExecution>(
    electricCollectionOptions({
      id: `sync-executions-${orgId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "executions",
          where: `org_id = '${orgId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.execution_id),
    }) as any,
  );
}

// --- Execution log chunks (real-time streaming via Electric) ---

export interface ElectricExecutionLog {
  attempt_id: string;
  seq: number;
  stream: string;
  chunk: string;
  created_at: string;
}

export function createExecutionLogsCollection(attemptId: string) {
  return createCollection<ElectricExecutionLog>(
    electricCollectionOptions({
      id: `sync-execution-logs-${attemptId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "execution_logs",
          where: `attempt_id = '${attemptId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) =>
        `${String(item.attempt_id)}:${String(item.seq)}`,
    }) as any,
  );
}
