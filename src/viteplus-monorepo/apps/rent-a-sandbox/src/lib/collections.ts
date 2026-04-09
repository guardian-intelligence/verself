import {
  electricEqualsWhere,
  electricShapeURL,
  requireDecimalID,
  requireUUID,
} from "@forge-metal/web-env";
import { createElectricCollection } from "./electric";

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
  const validatedOrgID = requireDecimalID(orgId, "org_id");
  return createElectricCollection<ElectricExecution>({
    id: `sync-executions-${orgId}`,
    shapeOptions: {
      url: electricShapeURL(),
      params: {
        table: "executions",
        where: electricEqualsWhere("org_id", validatedOrgID),
      },
    },
    getKey: (item) => item.execution_id,
  });
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
  const validatedAttemptID = requireUUID(attemptId, "attempt_id");
  return createElectricCollection<ElectricExecutionLog>({
    id: `sync-execution-logs-${attemptId}`,
    shapeOptions: {
      url: electricShapeURL(),
      params: {
        table: "execution_logs",
        where: electricEqualsWhere("attempt_id", validatedAttemptID),
      },
    },
    getKey: (item) => `${item.attempt_id}:${item.seq}`,
  });
}
