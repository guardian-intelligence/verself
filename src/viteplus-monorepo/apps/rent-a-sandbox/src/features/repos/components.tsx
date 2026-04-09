import { Link } from "@tanstack/react-router";
import type { Repo, RepoCompatibilitySummary } from "~/server-fns/api";
import { Callout } from "~/components/callout";
import { EmptyState } from "~/components/empty-state";

export function RepoStateBadge({ state }: { state: string }) {
  const colors: Record<string, string> = {
    importing: "bg-slate-100 text-slate-800",
    action_required: "bg-amber-100 text-amber-900",
    waiting_for_bootstrap: "bg-yellow-100 text-yellow-900",
    preparing: "bg-sky-100 text-sky-900",
    ready: "bg-green-100 text-green-900",
    degraded: "bg-orange-100 text-orange-900",
    failed: "bg-red-100 text-red-900",
    archived: "bg-zinc-200 text-zinc-800",
  };

  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${colors[state] ?? "bg-muted text-muted-foreground"}`}
    >
      {state.replaceAll("_", " ")}
    </span>
  );
}

export function GenerationStateBadge({ state }: { state: string }) {
  const colors: Record<string, string> = {
    queued: "bg-yellow-100 text-yellow-900",
    building: "bg-sky-100 text-sky-900",
    sanitizing: "bg-indigo-100 text-indigo-900",
    ready: "bg-green-100 text-green-900",
    failed: "bg-red-100 text-red-900",
    superseded: "bg-zinc-200 text-zinc-800",
  };

  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${colors[state] ?? "bg-muted text-muted-foreground"}`}
    >
      {state.replaceAll("_", " ")}
    </span>
  );
}

export function RepoMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate font-medium">{value}</div>
    </div>
  );
}

export function RepoListEmptyState() {
  return (
    <EmptyState
      title="No repos imported yet"
      body={
        <>
          Start by importing a repository that uses <code>runs-on: forge-metal</code>.
        </>
      }
      action={
        <Link
          to="/repos/new"
          className="inline-flex rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90"
        >
          Import Your First Repo
        </Link>
      }
    />
  );
}

export function RepoListLoadingState() {
  return (
    <EmptyState
      title="Loading repos..."
      body="Synchronizing the latest repository state."
    />
  );
}

export function RepoDetailLoadingState() {
  return (
    <EmptyState
      title="Loading repo..."
      body="Fetching the repo record and generation history."
    />
  );
}

export function RepoErrorBanner({ message }: { message: string }) {
  return (
    <Callout tone="destructive" title="Repository error">
      {message}
    </Callout>
  );
}

export function RepoListItem({ repo }: { repo: Repo }) {
  const issues = repo.compatibility_summary?.issues ?? [];

  return (
    <Link
      to="/repos/$repoId"
      params={{ repoId: repo.repo_id }}
      className="rounded-lg border border-border p-5 transition-colors hover:bg-accent/30"
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="truncate text-lg font-semibold">{repo.full_name}</h2>
            <RepoStateBadge state={repo.state} />
          </div>
          <p className="truncate text-sm text-muted-foreground">{repo.clone_url}</p>
        </div>
        <div className="text-right text-sm text-muted-foreground">
          <div>Profile: {repo.runner_profile_slug}</div>
          <div>Default branch: {repo.default_branch}</div>
        </div>
      </div>

      <div className="mt-4 grid gap-3 text-sm md:grid-cols-4">
        <RepoMetric
          label="Compatibility"
          value={
            repo.compatibility_status === "compatible" ? "Compatible" : repo.compatibility_status || "--"
          }
        />
        <RepoMetric label="Last scanned" value={shortSHA(repo.last_scanned_sha)} />
        <RepoMetric label="Active golden" value={shortID(repo.active_golden_generation_id)} />
        <RepoMetric label="Last ready" value={shortSHA(repo.last_ready_sha)} />
      </div>

      {repo.last_error || issues.length > 0 ? (
        <div className="mt-4 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
          {repo.last_error ? repo.last_error : `${issues.length} workflow issue(s) need attention`}
        </div>
      ) : null}
    </Link>
  );
}

export function RepoCompatibilityPanel({ summary }: { summary: RepoCompatibilitySummary | undefined }) {
  const issues = summary?.issues ?? [];
  const labels = summary?.unsupported_labels ?? [];
  const paths = summary?.workflow_paths ?? [];

  return (
    <div className="space-y-3">
      <h2 className="text-lg font-semibold">Workflow Compatibility</h2>
      <div className="space-y-4 rounded-lg border border-border p-4">
        <div className="grid gap-4 text-sm md:grid-cols-3">
          <RepoMetric label="Workflows" value={paths.length > 0 ? String(paths.length) : "0"} />
          <RepoMetric
            label="Unsupported labels"
            value={labels.length > 0 ? labels.join(", ") : "none"}
          />
          <RepoMetric label="Issues" value={issues.length > 0 ? String(issues.length) : "0"} />
        </div>

        {paths.length > 0 ? (
          <div>
            <h3 className="mb-2 text-sm font-medium">Workflow files</h3>
            <div className="flex flex-wrap gap-2">
              {paths.map((path) => (
                <code key={path} className="rounded bg-muted px-2 py-1 text-xs">
                  {path}
                </code>
              ))}
            </div>
          </div>
        ) : null}

        {issues.length > 0 ? (
          <div className="space-y-2">
            <h3 className="text-sm font-medium">Issues</h3>
            {issues.map((issue, index) => (
              <div key={`${issue.path}-${issue.job_id}-${index}`} className="rounded-md border border-border p-3 text-sm">
                <div className="font-medium">
                  {issue.path || "workflow"} {issue.job_id ? `· ${issue.job_id}` : ""}
                </div>
                <div className="mt-1 text-muted-foreground">
                  {issue.reason}
                  {issue.details ? `: ${issue.details}` : ""}
                </div>
                {issue.labels && issue.labels.length > 0 ? (
                  <div className="mt-2 flex flex-wrap gap-2">
                    {issue.labels.map((label) => (
                      <code key={label} className="rounded bg-muted px-2 py-1 text-xs">
                        {label}
                      </code>
                    ))}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            No compatibility issues recorded on the latest scan.
          </p>
        )}
      </div>
    </div>
  );
}

function shortSHA(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 12);
}

function shortID(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 8);
}
