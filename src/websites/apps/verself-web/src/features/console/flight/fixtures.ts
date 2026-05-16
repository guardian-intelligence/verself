import type { FlightRow } from "./collection";
import { toFlights, type Flight } from "./model";

// Deterministic mock states for `aspect dev verself-web` + agent-browser +
// design review. Named fixtures push raw string rows through model.ts (so the
// data contract is exercised); `?flight=debug` seeds one card straight from
// query params. No backend, no "cold"/"diverted"/"landed" — only the phases
// the product tracks: boarding, enroute (on time), late.
export type FlightFixtureName = "no-build" | "boarding" | "building-on-time" | "running-late";

export const FLIGHT_FIXTURE_NAMES: readonly FlightFixtureName[] = [
  "no-build",
  "boarding",
  "building-on-time",
  "running-late",
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
    case "boarding":
      rows = [job({ provider_job_id: "1", status: "queued", started_at: null })];
      break;
    case "building-on-time":
      rows = [job({ provider_job_id: "1", job_name: "test", started_at: isoAgo(180) })];
      break;
    case "running-late":
      rows = [
        job({
          provider_job_id: "1",
          job_name: "integration",
          started_at: isoAgo(900),
          predicted_baseline_ms: "600000",
        }),
      ];
      break;
  }
  return toFlights(rows);
}

// Single card seeded entirely from query params: `?flight=debug`.
export type DebugFlightParams = {
  readonly actor?: string | undefined;
  readonly src?: string | undefined;
  readonly dst?: string | undefined;
  readonly status?: string | undefined;
  readonly state?: string | undefined; // "ontime" | "late"
  readonly remaining?: string | undefined; // minutes (state=ontime)
  readonly commits?: string | undefined; // number, or "none"
};

export function debugFlight(p: DebugFlightParams): readonly Flight[] {
  const now = Date.now();
  const state = p.state ?? "ontime";
  let departedAtMs = now;
  let baselineMs: number;
  if (state === "late") {
    baselineMs = 60_000;
    departedAtMs = now - 120_000;
  } else {
    const minutes = Math.max(0, Number.parseInt(p.remaining ?? "12", 10) || 12);
    baselineMs = minutes * 60_000;
    departedAtMs = now;
  }
  const commitsRaw = p.commits ?? "4";
  const commits = Number.parseInt(commitsRaw, 10);
  const commitPill =
    commitsRaw === "none" || !Number.isFinite(commits) || commits <= 0
      ? null
      : commits > 99
        ? "99+"
        : String(commits);

  return [
    {
      key: "debug",
      providerRunId: "debug",
      actorLabel: (p.actor ?? "ash").toUpperCase(),
      sourceLabel: (p.src ?? "PR47").toUpperCase(),
      destLabel: (p.dst ?? "MAIN").toUpperCase(),
      statusLabel: p.status ?? "Building",
      departedAtMs,
      baselineMs,
      commitPill,
    },
  ];
}
