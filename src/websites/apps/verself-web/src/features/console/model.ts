import type { Execution, RunsPage } from "@verself/sdk";

// Status reduced to what an ambient timeline dot needs to express. The commit
// *title* lands with the separate runs-contract slice; until then the label is
// the run's workflow/job identity, which is what the runner metadata carries.
export type ConsoleRunStatus = "passed" | "failed" | "running" | "queued" | "canceled" | "unknown";

export type ConsoleCommit = {
  readonly key: string;
  readonly label: string;
  readonly sha: string;
  readonly status: ConsoleRunStatus;
  readonly updatedAt: string;
};

export type ConsoleRepo = {
  readonly key: string;
  readonly name: string;
  readonly fullName: string;
  readonly commits: readonly ConsoleCommit[];
};

export type ConsoleView = {
  readonly repos: readonly ConsoleRepo[];
};

const COMMITS_PER_REPO = 6;

export function toConsoleView(page: RunsPage): ConsoleView {
  const byRepo = new Map<string, { fullName: string; commits: ConsoleCommit[] }>();

  for (const run of page.runs) {
    const fullName = repositoryFullName(run);
    const group = byRepo.get(fullName) ?? { fullName, commits: [] };
    group.commits.push(toCommit(run));
    byRepo.set(fullName, group);
  }

  const repos = Array.from(byRepo.values())
    .map((group) => {
      const commits = group.commits
        .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
        .slice(0, COMMITS_PER_REPO);
      return {
        key: group.fullName,
        name: lastPathSegment(group.fullName),
        fullName: group.fullName,
        commits,
      };
    })
    .sort((a, b) => latestMs(b.commits) - latestMs(a.commits));

  return { repos };
}

function toCommit(run: Execution): ConsoleCommit {
  return {
    key: run.execution_id,
    label:
      text(run.runner?.job_name) ||
      text(run.runner?.workflow_name) ||
      text(run.run_command) ||
      text(run.schedule?.display_name) ||
      "Sandbox run",
    sha: text(run.runner?.head_sha).slice(0, 7),
    status: statusOf(run.status),
    updatedAt: run.updated_at,
  };
}

function statusOf(status: string): ConsoleRunStatus {
  const normalized = status.trim().toLowerCase();
  if (["ready", "success", "succeeded", "completed", "complete"].includes(normalized)) {
    return "passed";
  }
  if (["error", "failed", "failure"].includes(normalized)) return "failed";
  if (["running", "active", "in_progress"].includes(normalized)) return "running";
  if (["queued", "pending", "scheduled"].includes(normalized)) return "queued";
  if (["canceled", "cancelled"].includes(normalized)) return "canceled";
  return "unknown";
}

function repositoryFullName(run: Execution): string {
  return text(run.runner?.repository_full_name) || text(run.source_ref) || "Unknown repository";
}

function latestMs(commits: readonly ConsoleCommit[]): number {
  const newest = commits.at(0);
  return newest ? Date.parse(newest.updatedAt) : 0;
}

function lastPathSegment(value: string): string {
  const segments = value.split("/").filter(Boolean);
  return segments.at(-1) ?? value;
}

function text(value: string | undefined): string {
  return value?.trim() ?? "";
}
