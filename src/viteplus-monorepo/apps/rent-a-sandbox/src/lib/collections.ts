import { createCollection } from "@tanstack/db";
import { electricCollectionOptions } from "@tanstack/electric-db-collection";

// Electric shape URL — proxied through Caddy at the same origin.
// Electric syncs the sandbox_rental database only. Billing data
// (subscriptions, grants, balance) uses TanStack Query — balance is
// TigerBeetle-derived and billing tables live in a separate PG database.
const ELECTRIC_SHAPE_URL = "/v1/shape";

// --- Job collection (real-time PG sync via Electric) ---

export interface ElectricJob {
  id: string;
  org_id: string;
  user_id: string;
  repo_url: string;
  run_command: string | null;
  status: string;
  exit_code: number | null;
  duration_ms: number | null;
  zfs_written: number | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
}

export function createJobsCollection(orgId: string) {
  return createCollection<ElectricJob>(
    electricCollectionOptions({
      id: `sync-jobs-${orgId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "jobs",
          where: `org_id = '${orgId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.id),
    }) as any,
  );
}

// --- Job log chunks (real-time streaming via Electric) ---

export interface ElectricJobLog {
  id: string;
  job_id: string;
  seq: number;
  stream: string;
  chunk: string;
  created_at: string;
}

export function createJobLogsCollection(jobId: string) {
  return createCollection<ElectricJobLog>(
    electricCollectionOptions({
      id: `sync-job-logs-${jobId}`,
      shapeOptions: {
        url: ELECTRIC_SHAPE_URL,
        params: {
          table: "job_logs",
          where: `job_id = '${jobId}'`,
        },
      },
      getKey: (item: Record<string, unknown>) => String(item.id),
    }) as any,
  );
}
