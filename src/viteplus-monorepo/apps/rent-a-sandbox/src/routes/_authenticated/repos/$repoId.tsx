import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Callout } from "~/components/callout";
import { ErrorCallout } from "~/components/error-callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import {
  GenerationStateBadge,
  RepoCompatibilityPanel,
  RepoMetric,
  RepoStateBadge,
  shortSHA,
} from "~/features/repos/components";
import {
  canRefresh,
  canRun,
  loadRepoDetail,
  repoGenerationsQuery,
  repoQuery,
} from "~/features/repos/queries";
import {
  useRefreshRepoMutation,
  useRescanRepoMutation,
  useRunRepoExecutionMutation,
} from "~/features/repos/mutations";

export const Route = createFileRoute("/_authenticated/repos/$repoId")({
  loader: ({ context, params }) => loadRepoDetail(context.queryClient, params.repoId),
  component: RepoDetailPage,
});

function RepoDetailPage() {
  const { repoId } = Route.useParams();
  const navigate = useNavigate();
  const repo = useSuspenseQuery(repoQuery(repoId)).data;
  const generations = useSuspenseQuery(repoGenerationsQuery(repoId)).data;

  const rescanMutation = useRescanRepoMutation(repoId);
  const refreshMutation = useRefreshRepoMutation(repoId);
  const runMutation = useRunRepoExecutionMutation(repoId, (executionId) => {
    void navigate({ to: "/jobs/$jobId", params: { jobId: executionId } });
  });

  const activeGeneration = generations.find(
    (generation) => generation.golden_generation_id === repo.active_golden_generation_id,
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <Link to="/repos" className="text-sm text-muted-foreground hover:text-foreground">
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
            className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent disabled:opacity-50"
          >
            {rescanMutation.isPending ? "Rescanning..." : "Rescan"}
          </button>
          <button
            type="button"
            onClick={() => refreshMutation.mutate()}
            disabled={refreshMutation.isPending || !canRefresh(repo)}
            className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent disabled:opacity-50"
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
            className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            {runMutation.isPending ? "Launching..." : "Run Execution"}
          </button>
        </div>
      </div>

      {rescanMutation.error ? (
        <ErrorCallout error={rescanMutation.error} title="Rescan failed" />
      ) : null}
      {refreshMutation.error ? (
        <ErrorCallout error={refreshMutation.error} title="Refresh failed" />
      ) : null}
      {runMutation.error ? (
        <ErrorCallout error={runMutation.error} title="Execution launch failed" />
      ) : null}
      {repo.last_error ? (
        <Callout tone="warning" title="Repository reported an error">
          {repo.last_error}
        </Callout>
      ) : null}

      <div className="grid gap-4 md:grid-cols-4">
        <RepoMetric label="Provider" value={repo.provider} />
        <RepoMetric label="Default branch" value={repo.default_branch} />
        <RepoMetric label="Runner profile" value={repo.runner_profile_slug} />
        <RepoMetric label="Last scanned SHA" value={shortSHA(repo.last_scanned_sha)} />
        <RepoMetric label="Last ready SHA" value={shortSHA(repo.last_ready_sha)} />
        <RepoMetric
          label="Active golden"
          value={
            repo.active_golden_generation_id ? repo.active_golden_generation_id.slice(0, 8) : "--"
          }
        />
        <RepoMetric label="Compatibility" value={repo.compatibility_status || "--"} />
        <RepoMetric label="Updated" value={new Date(repo.updated_at).toLocaleString()} />
      </div>

      <RepoCompatibilityPanel summary={repo.compatibility_summary} />

      <div className="grid gap-6 lg:grid-cols-2">
        <section className="space-y-3">
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
          <div className="rounded-lg border border-border p-4">
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
        </section>

        <section className="space-y-3">
          <h2 className="text-lg font-semibold">Repo Contract</h2>
          <div className="space-y-2 rounded-lg border border-border p-4 text-sm text-muted-foreground">
            <p>
              v1 supports a single runner label and profile: <code>forge-metal</code>.
            </p>
            <p>
              Default-branch compatibility gates bootstrap. Once a ready golden exists, future repo
              executions use that active snapshot.
            </p>
            <p>
              Manual refresh builds a new generation in the background and only activates it after
              the generation succeeds.
            </p>
          </div>
        </section>
      </div>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Golden Generations</h2>
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-2 text-left font-medium">Generation</th>
                <th className="px-4 py-2 text-left font-medium">State</th>
                <th className="px-4 py-2 text-left font-medium">Trigger</th>
                <th className="px-4 py-2 text-left font-medium">Source SHA</th>
                <th className="px-4 py-2 text-left font-medium">Execution</th>
                <th className="px-4 py-2 text-left font-medium">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {generations.length === 0 ? (
                <TableEmptyRow
                  colSpan={6}
                  title="No generations yet"
                  description="Prepare the repo once the default branch is compatible."
                />
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
                          className="font-mono text-primary hover:underline"
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
      </section>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="break-all text-right font-medium">{value}</span>
    </div>
  );
}
