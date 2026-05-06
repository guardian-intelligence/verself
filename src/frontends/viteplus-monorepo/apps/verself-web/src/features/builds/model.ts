import * as v from "valibot";
import {
  runListQuerySchema,
  type Execution,
  type RunListQueryInput,
  type RunsPage,
} from "~/lib/sandbox-rental-api";

export const BUILD_RUNS_PAGE_SIZE = 50;
export const DEFAULT_BUILD_RUNS_QUERY = { limit: BUILD_RUNS_PAGE_SIZE } satisfies RunListQueryInput;

const shortDateFormatter = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  month: "numeric",
  timeZone: "UTC",
  year: "2-digit",
});

export type BuildRunStatusTone = "ready" | "error" | "running" | "queued" | "canceled" | "muted";

export type BuildRunRow = {
  readonly actorInitial: string;
  readonly actorLabel: string;
  readonly branch: string;
  readonly commitLabel: string;
  readonly commitSha: string;
  readonly durationLabel: string;
  readonly environment: string;
  readonly executionId: string;
  readonly projectLabel: string;
  readonly providerLabel: string;
  readonly repositoryFullName: string;
  readonly repositoryLabel: string;
  readonly runDateLabel: string;
  readonly shortId: string;
  readonly showCurrent: boolean;
  readonly sourceLabel: string;
  readonly statusLabel: string;
  readonly statusTone: BuildRunStatusTone;
};

export type BuildsDashboardView = {
  readonly filters: RunsPage["filters"];
  readonly hasNextPage: boolean;
  readonly nextCursor: string;
  readonly rows: readonly BuildRunRow[];
};

export function normalizeBuildRunsQuery(query: RunListQueryInput = DEFAULT_BUILD_RUNS_QUERY) {
  return v.parse(runListQuerySchema, { ...DEFAULT_BUILD_RUNS_QUERY, ...query });
}

export function toBuildsDashboardView(page: RunsPage): BuildsDashboardView {
  return {
    filters: page.filters,
    hasNextPage: page.nextCursor.length > 0,
    nextCursor: page.nextCursor,
    rows: page.runs.map((run) => toBuildRunRow(run)),
  };
}

function toBuildRunRow(run: Execution): BuildRunRow {
  const status = statusView(run.status);
  const branch = normalizeText(run.runner?.head_branch) || branchFromSourceRef(run.source_ref);
  const repositoryFullName =
    normalizeText(run.runner?.repository_full_name) ||
    normalizeText(run.source_ref) ||
    "Unknown repository";
  const repositoryLabel = lastPathSegment(repositoryFullName) || repositoryFullName;
  const commitSha = normalizeText(run.runner?.head_sha).slice(0, 7) || "pending";
  const commitLabel =
    normalizeText(run.runner?.job_name) ||
    normalizeText(run.runner?.workflow_name) ||
    normalizeText(run.run_command) ||
    normalizeText(run.schedule?.display_name) ||
    "Sandbox run";
  const actorLabel = actorDisplayName(run.actor_id);
  return {
    actorInitial: actorLabel.charAt(0).toUpperCase() || "V",
    actorLabel,
    branch,
    commitLabel,
    commitSha,
    durationLabel: durationLabel(run),
    environment: environmentLabel(branch),
    executionId: run.execution_id,
    projectLabel: repositoryLabel,
    providerLabel: providerLabel(run),
    repositoryFullName,
    repositoryLabel,
    runDateLabel: shortDate(run.updated_at),
    shortId: run.run_id.slice(0, 10) || run.execution_id.slice(0, 10),
    showCurrent: status.tone === "ready" && isProductionBranch(branch),
    sourceLabel: sourceLabel(run),
    statusLabel: status.label,
    statusTone: status.tone,
  };
}

function statusView(status: string): { readonly label: string; readonly tone: BuildRunStatusTone } {
  const normalized = status.trim().toLowerCase();
  if (["ready", "success", "succeeded", "completed", "complete"].includes(normalized)) {
    return { label: "Ready", tone: "ready" };
  }
  if (["error", "failed", "failure"].includes(normalized)) {
    return { label: "Error", tone: "error" };
  }
  if (["running", "active", "in_progress"].includes(normalized)) {
    return { label: "Building", tone: "running" };
  }
  if (["queued", "pending", "scheduled"].includes(normalized)) {
    return { label: "Queued", tone: "queued" };
  }
  if (["canceled", "cancelled"].includes(normalized)) {
    return { label: "Canceled", tone: "canceled" };
  }
  return { label: toTitleCase(normalized || "unknown"), tone: "muted" };
}

function durationLabel(run: Execution): string {
  const durationMs = run.latest_attempt.duration_ms;
  if (durationMs !== undefined) return formatDuration(durationMs);
  const startedAt = Date.parse(run.latest_attempt.started_at ?? run.created_at);
  const completedAt = Date.parse(run.latest_attempt.completed_at ?? run.updated_at);
  if (!Number.isFinite(startedAt) || !Number.isFinite(completedAt) || completedAt < startedAt) {
    return "0s";
  }
  return formatDuration(completedAt - startedAt);
}

function formatDuration(durationMs: number): string {
  const totalSeconds = Math.max(0, Math.round(durationMs / 1000));
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  return remainingMinutes === 0 ? `${hours}h` : `${hours}h ${remainingMinutes}m`;
}

function environmentLabel(branch: string): string {
  return isProductionBranch(branch) ? "Production" : "Preview";
}

function isProductionBranch(branch: string): boolean {
  const normalized = branch.trim().toLowerCase();
  return normalized === "main" || normalized === "master" || normalized === "production";
}

function branchFromSourceRef(sourceRef: string | undefined): string {
  const normalized = normalizeText(sourceRef);
  if (!normalized) return "main";
  return normalized.replace(/^refs\/heads\//, "") || "main";
}

function providerLabel(run: Execution): string {
  const source =
    normalizeText(run.external_provider) ||
    normalizeText(run.provider) ||
    normalizeText(run.source_kind);
  if (!source) return "Verself";
  if (source.toLowerCase() === "github") return "GitHub";
  return toTitleCase(source);
}

function sourceLabel(run: Execution): string {
  const source = normalizeText(run.source_kind) || normalizeText(run.workload_kind) || run.kind;
  return toTitleCase(source);
}

function actorDisplayName(actorId: string): string {
  const normalized = actorId.trim();
  if (!normalized) return "Verself";
  if (/^[0-9a-f]{8}-[0-9a-f-]{27,}$/i.test(normalized)) return "Verself";
  const withoutProvider = normalized.replace(/^zitadel:/, "").replace(/^user:/, "");
  return withoutProvider.length > 18 ? `${withoutProvider.slice(0, 18)}...` : withoutProvider;
}

function lastPathSegment(value: string): string {
  const segments = value.split("/").filter(Boolean);
  return segments.at(-1) ?? value;
}

function shortDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Invalid date" : shortDateFormatter.format(date);
}

function normalizeText(value: string | undefined): string {
  return value?.trim() ?? "";
}

function toTitleCase(value: string): string {
  return value
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => `${part.charAt(0).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}
