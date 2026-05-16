import * as v from "valibot";
import { authCollectionId, type AuthenticatedAuth } from "@verself/auth-web/isomorphic";
import { createElectricShapeCollection } from "@verself/web-env";

// TanStack DB does NOT run schema transforms on synced rows, and Electric
// serializes every Postgres column as a JSON string (NULL -> null). So this
// schema is intentionally all raw strings: it drives type inference, getKey,
// and collection identity only. All coercion (numbers, timestamps) happens in
// model.ts, never here.
const flightRowSchema = v.object({
  provider: v.string(),
  provider_job_id: v.string(),
  org_id: v.string(),
  provider_run_id: v.string(),
  provider_run_attempt: v.string(),
  repository_full_name: v.string(),
  workflow_name: v.string(),
  job_name: v.string(),
  head_branch: v.string(),
  head_sha: v.string(),
  pr_number: v.string(),
  base_branch: v.string(),
  actor_login: v.string(),
  commit_count: v.nullable(v.string()),
  status: v.string(),
  predicted_baseline_ms: v.nullable(v.string()),
  created_at: v.string(),
  started_at: v.nullable(v.string()),
});

export type FlightRow = v.InferOutput<typeof flightRowSchema>;

// Module-level cache keyed by the stable collection id. Electric shapes are
// durable subscriptions the server tracks by id; without this, any remount
// (e.g. back-nav) restarts the useMemo and opens a fresh subscription that
// only resolves after a round-trip (idle -> loading flicker). Consumers are
// <ClientOnly>-gated so this map only ever populates in the browser.
const flightCollectionCache = new Map<string, unknown>();

export function createFlightsCollection(auth: AuthenticatedAuth) {
  const id = authCollectionId(auth, "sync-flights");
  const existing = flightCollectionCache.get(id);
  if (existing !== undefined) {
    return existing as ReturnType<typeof createElectricShapeCollection<typeof flightRowSchema>>;
  }
  const collection = createElectricShapeCollection({
    id,
    schema: flightRowSchema,
    shapePath: "/api/sync/flights",
    getKey: (item) => `${item.provider}:${item.provider_job_id}`,
  });
  flightCollectionCache.set(id, collection);
  return collection;
}
