import type { FlightRow } from "./collection";

// One card per in-flight workflow run. Rows are job-grained; a flight is the
// group of a run's active jobs (the Electric shape only syncs active jobs, so
// every synced row is "in flight" and "jobs left" is just the group size).
export type Flight = {
  readonly key: string;
  readonly providerRunId: string;
  readonly actorLabel: string;
  readonly sourceLabel: string;
  readonly destLabel: string;
  readonly statusLabel: string;
  readonly jobsLeft: number;
  // Epoch ms of the run's first job sighting; the elapsed clock anchors here.
  readonly departedAtMs: number;
  // p50 of this run's slowest job (critical path), frozen at departure.
  // null = cold start: the badge can never flip to "Running Late".
  readonly baselineMs: number | null;
  // Display-clamped PR commit count, or null (non-PR run -> pill omitted).
  readonly commitPill: string | null;
};

const ACTIVE_IN_PROGRESS = "in_progress";
const ACTIVE_WAITING = "waiting";

export function toFlights(rows: readonly FlightRow[]): readonly Flight[] {
  const byRun = new Map<string, FlightRow[]>();
  for (const row of rows) {
    const group = byRun.get(row.provider_run_id);
    if (group) group.push(row);
    else byRun.set(row.provider_run_id, [row]);
  }

  const flights: Flight[] = [];
  for (const jobs of byRun.values()) {
    const [head] = jobs;
    if (head) flights.push(toFlight(head, jobs));
  }
  // Newest departure first: a freshly opened build lands at the top.
  return flights.sort((a, b) => b.departedAtMs - a.departedAtMs);
}

function toFlight(head: FlightRow, jobs: readonly FlightRow[]): Flight {
  const prNumber = toInt(head.pr_number);
  const headBranch = branchLabel(head.head_branch) || "build";
  const baseBranch = branchLabel(head.base_branch);

  return {
    key: head.provider_run_id,
    providerRunId: head.provider_run_id,
    actorLabel: (firstNonEmpty(jobs, "actor_login") || "unknown").toUpperCase(),
    sourceLabel: prNumber > 0 ? `PR${prNumber}` : headBranch.toUpperCase(),
    destLabel: (baseBranch || headBranch).toUpperCase(),
    statusLabel: runStatusLabel(jobs),
    jobsLeft: jobs.length,
    departedAtMs: departedAtMs(jobs),
    baselineMs: runBaselineMs(jobs),
    commitPill: commitPill(head.commit_count),
  };
}

function runStatusLabel(jobs: readonly FlightRow[]): string {
  if (jobs.some((job) => job.status === ACTIVE_IN_PROGRESS)) return "Building";
  if (jobs.some((job) => job.status === ACTIVE_WAITING)) return "Waiting";
  return "Queued";
}

function departedAtMs(jobs: readonly FlightRow[]): number {
  let earliest = Number.POSITIVE_INFINITY;
  for (const job of jobs) {
    const ts = epochMs(job.started_at) ?? epochMs(job.created_at) ?? Number.POSITIVE_INFINITY;
    if (ts < earliest) earliest = ts;
  }
  return Number.isFinite(earliest) ? earliest : Date.now();
}

// The run is "late" once it exceeds the critical-path job's typical time, so
// take the max p50 across jobs that have a baseline. All-null -> cold start.
function runBaselineMs(jobs: readonly FlightRow[]): number | null {
  let max: number | null = null;
  for (const job of jobs) {
    const baseline = epochInt(job.predicted_baseline_ms);
    if (baseline !== null && (max === null || baseline > max)) max = baseline;
  }
  return max;
}

function commitPill(raw: string | null): string | null {
  const count = epochInt(raw);
  if (count === null || count <= 0) return null;
  return count > 99 ? "99+" : String(count);
}

// Estimated time remaining = frozen p50 baseline minus elapsed. null when
// there is no baseline (cold start) — the widget then shows status only.
export function remainingMs(flight: Flight, nowMs: number): number | null {
  if (flight.baselineMs === null) return null;
  return flight.baselineMs - (nowMs - flight.departedAtMs);
}

// Minutes only, as the user specified: "<1 min" floor, ">60 min" ceiling.
export function formatRemaining(ms: number): string {
  if (ms < 60_000) return "<1 min";
  const minutes = Math.round(ms / 60_000);
  if (minutes > 60) return ">60 min";
  return `${minutes} min`;
}

export function isRunningLate(flight: Flight, nowMs: number): boolean {
  return flight.baselineMs !== null && nowMs - flight.departedAtMs > flight.baselineMs;
}

function firstNonEmpty(jobs: readonly FlightRow[], key: "actor_login"): string {
  for (const job of jobs) {
    const value = job[key].trim();
    if (value) return value;
  }
  return "";
}

function branchLabel(ref: string): string {
  return ref.replace(/^refs\/heads\//, "").trim();
}

function toInt(raw: string): number {
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : 0;
}

function epochInt(raw: string | null): number | null {
  if (raw === null || raw.trim() === "") return null;
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : null;
}

function epochMs(raw: string | null): number | null {
  if (raw === null || raw.trim() === "") return null;
  const value = Date.parse(raw);
  return Number.isNaN(value) ? null : value;
}
