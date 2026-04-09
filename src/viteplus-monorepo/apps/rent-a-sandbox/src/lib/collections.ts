import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

// Electric requires an absolute shape URL. Keep the real sync path same-origin
// in the browser, but return a harmless absolute fallback during SSR so the URL
// parser never sees a bare relative path.
function electricShapeURL(): string {
  if (typeof window !== "undefined" && window.location?.origin) {
    return new URL("/v1/shape", window.location.origin).toString();
  }
  return "http://127.0.0.1/v1/shape";
}

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
        url: electricShapeURL(),
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
        url: electricShapeURL(),
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
