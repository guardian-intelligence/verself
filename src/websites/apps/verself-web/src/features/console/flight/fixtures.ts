import type { FlightRow } from "./collection";
import { toFlights, type Flight } from "./model";

// Deterministic states for `aspect dev verself-web` + agent-browser. Raw rows
// (strings, like Electric delivers) so the fixtures exercise model.ts too.
// Timestamps are relative to call time so elapsed/badge stay deterministic
// whenever the fixture is loaded.
export type FlightFixtureName = "no-build" | "building-on-time" | "running-late" | "no-history";

export const FLIGHT_FIXTURE_NAMES: readonly FlightFixtureName[] = [
  "no-build",
  "building-on-time",
  "running-late",
  "no-history",
];

export function isFlightFixtureName(value: string): value is FlightFixtureName {
  return (FLIGHT_FIXTURE_NAMES as readonly string[]).includes(value);
}

function job(overrides: Partial<FlightRow> & Pick<FlightRow, "provider_job_id">): FlightRow {
  return {
    provider: "github",
    org_id: "fixture-org",
    provider_run_id: "9001",
    provider_run_attempt: "1",
    repository_full_name: "guardian-intelligence/verself-sh",
    workflow_name: "CI",
    job_name: "test",
    head_branch: "feat/flight-tracker",
    head_sha: "0000000",
    pr_number: "47",
    base_branch: "main",
    actor_login: "ash",
    commit_count: "4",
    status: "in_progress",
    predicted_baseline_ms: "900000",
    created_at: new Date().toISOString(),
    started_at: new Date().toISOString(),
    ...overrides,
  };
}

function isoAgo(secondsAgo: number): string {
  return new Date(Date.now() - secondsAgo * 1000).toISOString();
}

export function flightFixture(name: FlightFixtureName): readonly Flight[] {
  let rows: FlightRow[];
  switch (name) {
    case "no-build":
      rows = [];
      break;
    case "building-on-time":
      rows = [
        job({ provider_job_id: "1", job_name: "test-node-20", started_at: isoAgo(12) }),
        job({ provider_job_id: "2", job_name: "test-node-22", started_at: isoAgo(11) }),
        job({ provider_job_id: "3", job_name: "lint", status: "queued", started_at: null }),
        job({ provider_job_id: "4", job_name: "integration", started_at: isoAgo(9) }),
      ];
      break;
    case "running-late":
      rows = [
        job({
          provider_job_id: "5",
          job_name: "integration",
          started_at: isoAgo(130),
          predicted_baseline_ms: "60000",
        }),
      ];
      break;
    case "no-history":
      rows = [
        job({
          provider_job_id: "6",
          job_name: "build-docker",
          pr_number: "0",
          base_branch: "",
          head_branch: "main",
          commit_count: null,
          predicted_baseline_ms: null,
          started_at: isoAgo(45),
        }),
      ];
      break;
  }
  return toFlights(rows);
}
