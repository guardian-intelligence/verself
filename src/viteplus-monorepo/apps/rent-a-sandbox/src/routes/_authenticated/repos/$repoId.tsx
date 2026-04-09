import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Suspense } from "react";
import { RepoStateBadge, shortSHA } from "~/components/repo-state";
import { RepoDetailLoadingState, RepoErrorBanner } from "~/features/repos/components";
import {
  loadRepoDetail,
  repoGenerationsQuery,
  repoQuery,
} from "~/features/repos/queries";
import {
  useRefreshRepoMutation,
  useRescanRepoMutation,
  useRunRepoExecutionMutation,
} from "~/features/repos/mutations";
import { requireViewer } from "~/lib/protected-route";
import type { Repo, RepoCompatibilitySummary } from "~/server-fns/api";

export const Route = createFileRoute("/repos/$repoId")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context, params }) => loadRepoDetail(context.queryClient, params.repoId),
  component: RepoDetailPage,
});

function RepoDetailPage() {
  return (
    <Suspense fallback={<RepoDetailLoadingState />}>
      <RepoDetailContent />
    </Suspense>
  );
}

function RepoDetailContent() {
  const { repoId } = Route.useParams();
  const navigate = useNavigate();
  const { data: repo } = useSuspenseQuery(repoQuery(repoId));
  const { data: generations } = useSuspenseQuery(repoGenerationsQuery(repoId));
  const rescanMutation = useRescanRepoMutation(repoId);
  const refreshMutation = useRefreshRepoMutation(repoId);
  const runMutation = useRunRepoExecutionMutation(repoId, {
    onSuccess: (data) => {
      void navigate({ to: "/jobs/$jobId", params: { jobId: data.execution_id } });
    },
  });

  const activeGeneration = generations.find(
    (generation) => generation.golden_generation_id === repo.active_golden_generation_id,
  );
  const summary = repo.compatibility_summary;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Link to="/repos" className="text-muted-foreground hover:text-foreground text-sm">
              Repos
            </Link>
            <span className="text-muted-foreground">/</span>
            <h1 className="text-2xl font-bold">{repo.full_name}</h1>
            <RepoStateBadge state={repo.state} />
          </div>
          <p className="text-sm text-muted-foreground">{repo.clone_url}</p>
        </div>

        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => rescanMutation.mutate()}
            disabled={rescanMutation.isPending}
            className="px-3 py-2 rounded-md border border-border hover:bg-accent text-sm disabled:opacity-50"
          >
            {rescanMutation.isPending ? "Rescanning..." : "Rescan"}
          </button>
          <button
            type="button"
            onClick={() => refreshMutation.mutate()}
            disabled={refreshMutation.isPending || !canRefresh(repo)}
            className="px-3 py-2 rounded-md border border-border hover:bg-accent text-sm disabled:opacity-50"
          >
            {refreshMutation.isPending
              ? "Queueing..."
              : repo.active_golden_generation_id
                ? "Refresh Golden"
                : "Prepare Golden"}
          </button>
          <button
            type="button"
            onClick={() => runMutation.mutate()}
            disabled={runMutation.isPending || !canRun(repo)}
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
          >
            {runMutation.isPending ? "Launching..." : "Run Execution"}
          </button>
        </div>
      </div>

      {rescanMutation.error ? <RepoErrorBanner message={rescanMutation.error.message} /> : null}
      {refreshMutation.error ? <RepoErrorBanner message={refreshMutation.error.message} /> : null}
      {runMutation.error ? <RepoErrorBanner message={runMutation.error.message} /> : null}
      {repo.last_error ? <RepoErrorBanner message={repo.last_error} /> : null}

      <div className="grid md:grid-cols-4 gap-4">
        <InfoCard label="Provider" value={repo.provider} />
        <InfoCard label="Default branch" value={repo.default_branch} />
        <InfoCard label="Runner profile" value={repo.runner_profile_slug} />
        <InfoCard label="Last scanned SHA" value={shortSHA(repo.last_scanned_sha)} />
        <InfoCard label="Last ready SHA" value={shortSHA(repo.last_ready_sha)} />
        <InfoCard
          label="Active golden"
          value={
            repo.active_golden_generation_id ? repo.active_golden_generation_id.slice(0, 8) : "--"
          }
        />
        <InfoCard label="Compatibility" value={repo.compatibility_status || "--"} />
        <InfoCard label="Updated" value={new Date(repo.updated_at).toLocaleString()} />
      </div>

      <CompatibilityPanel summary={summary ?? undefined} />

      <div className="grid lg:grid-cols-2 gap-6">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Active Golden</h2>
            {activeGeneration?.execution_id ? (
              <Link
                to="/jobs/$jobId"
                params={{ jobId: activeGeneration.execution_id }}
                className="text-sm text-primary hover:underline"
              >
                View bootstrap execution
              </Link>
            ) : null}
          </div>
          <div className="border border-border rounded-lg p-4">
            {activeGeneration ? (
              <div className="space-y-3 text-sm">
                <InfoRow
                  label="Generation"
                  value={activeGeneration.golden_generation_id.slice(0, 8)}
                />
                <InfoRow label="State" value={activeGeneration.state} />
                <InfoRow label="Source ref" value={activeGeneration.source_ref} />
                <InfoRow label="Source SHA" value={shortSHA(activeGeneration.source_sha)} />
                <InfoRow label="Snapshot" value={activeGeneration.snapshot_ref || "--"} />
                <InfoRow
                  label="Activated"
                  value={
                    activeGeneration.activated_at
                      ? new Date(activeGeneration.activated_at).toLocaleString()
                      : "--"
                  }
                />
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">
                No active golden yet. Prepare the repo once the default branch is compatible.
              </div>
            )}
          </div>
        </div>

        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Repo Contract</h2>
          <div className="border border-border rounded-lg p-4 text-sm text-muted-foreground space-y-2">
            <p>v1 supports a single runner label and profile: <code>forge-metal</code>.</p>
            <p>
              Default-branch compatibility gates bootstrap. Once a ready golden exists, future repo
              executions use that active snapshot.
            </p>
            <p>
              Manual refresh builds a new generation in the background and only activates it after
              the generation succeeds.
            </p>
          </div>
        </div>
      </div>

      <div className="space-y-3">
        <h2 className="text-lg font-semibold">Golden Generations</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Generation</th>
                <th className="text-left px-4 py-2 font-medium">State</th>
                <th className="text-left px-4 py-2 font-medium">Trigger</th>
                <th className="text-left px-4 py-2 font-medium">Source SHA</th>
                <th className="text-left px-4 py-2 font-medium">Execution</th>
                <th className="text-left px-4 py-2 font-medium">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {generations.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-6 text-center text-muted-foreground">
                    No generations yet.
                  </td>
                </tr>
              ) : (
                generations.map((generation) => (
                  <tr key={generation.golden_generation_id}>
                    <td className="px-4 py-2 font-mono">
                      {generation.golden_generation_id.slice(0, 8)}
                    </td>
                    <td className="px-4 py-2">
                      <GenerationStateBadge state={generation.state} />
                    </td>
                    <td className="px-4 py-2">{generation.trigger_reason}</td>
                    <td className="px-4 py-2 font-mono">{shortSHA(generation.source_sha)}</td>
                    <td className="px-4 py-2">
                      {generation.execution_id ? (
                        <Link
                          to="/jobs/$jobId"
                          params={{ jobId: generation.execution_id }}
                          className="text-primary hover:underline font-mono"
                        >
                          {generation.execution_id.slice(0, 8)}
                        </Link>
                      ) : (
                        "--"
                      )}
                    </td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {new Date(generation.created_at).toLocaleString()}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function CompatibilityPanel({ summary }: { summary: RepoCompatibilitySummary | undefined }) {
  const issues = summary?.issues ?? [];
  const labels = summary?.unsupported_labels ?? [];
  const paths = summary?.workflow_paths ?? [];

  return (
    <div className="space-y-3">
      <h2 className="text-lg font-semibold">Workflow Compatibility</h2>
      <div className="border border-border rounded-lg p-4 space-y-4">
        <div className="grid md:grid-cols-3 gap-4 text-sm">
          <InfoCard label="Workflows" value={paths.length > 0 ? String(paths.length) : "0"} />
          <InfoCard
            label="Unsupported labels"
            value={labels.length > 0 ? labels.join(", ") : "none"}
          />
          <InfoCard label="Issues" value={issues.length > 0 ? String(issues.length) : "0"} />
        </div>

        {paths.length > 0 ? (
          <div>
            <h3 className="font-medium text-sm mb-2">Workflow files</h3>
            <div className="flex flex-wrap gap-2">
              {paths.map((path) => (
                <code key={path} className="px-2 py-1 rounded bg-muted text-xs">
                  {path}
                </code>
              ))}
            </div>
          </div>
        ) : null}

        {issues.length > 0 ? (
          <div className="space-y-2">
            <h3 className="font-medium text-sm">Issues</h3>
            {issues.map((issue, index) => (
              <div
                key={`${issue.path}-${issue.job_id}-${index}`}
                className="rounded-md border border-border p-3 text-sm"
              >
                <div className="font-medium">
                  {issue.path || "workflow"} {issue.job_id ? `· ${issue.job_id}` : ""}
                </div>
                <div className="text-muted-foreground mt-1">
                  {issue.reason}
                  {issue.details ? `: ${issue.details}` : ""}
                </div>
                {issue.labels && issue.labels.length > 0 ? (
                  <div className="mt-2 flex flex-wrap gap-2">
                    {issue.labels.map((label) => (
                      <code key={label} className="px-2 py-1 rounded bg-muted text-xs">
                        {label}
                      </code>
                    ))}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        ) : (
          <div className="text-sm text-muted-foreground">
            No compatibility issues recorded on the latest scan.
          </div>
        )}
      </div>
    </div>
  );
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="border border-border rounded-lg p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="font-medium text-sm mt-1 truncate">{value}</div>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-right break-all">{value}</span>
    </div>
  );
}

function GenerationStateBadge({ state }: { state: string }) {
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
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[state] ?? "bg-muted text-muted-foreground"}`}
    >
      {state.replaceAll("_", " ")}
    </span>
  );
}

function canRefresh(repo: Repo): boolean {
  if (repo.compatibility_status !== "compatible") {
    return false;
  }
  return repo.state !== "archived" && repo.state !== "importing" && repo.state !== "preparing";
}

function canRun(repo: Repo): boolean {
  return (
    (repo.state === "ready" || repo.state === "degraded") && !!repo.active_golden_generation_id
  );
}
