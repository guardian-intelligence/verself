import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly, createFileRoute, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Callout } from "~/components/callout";
import { ErrorCallout } from "~/components/error-callout";
import {
  RepoCompatibilityPanel,
  RepoMetric,
  RepoStateBadge,
  shortSHA,
} from "~/features/repos/components";
import { canRefresh, loadRepoDetail, repoQuery } from "~/features/repos/queries";
import { useRescanRepoMutation } from "~/features/repos/mutations";
import { formatDateTimeUTC } from "~/lib/format";
import type { Repo } from "~/lib/sandbox-rental-api";

export const Route = createFileRoute("/_authenticated/repos/$repoId")({
  loader: ({ context, params }) => loadRepoDetail(context.queryClient, context.auth, params.repoId),
  component: RepoDetailPage,
});

function RepoDetailPage() {
  const auth = useSignedInAuth();
  const { repoId } = Route.useParams();
  const repo = useSuspenseQuery(repoQuery(auth, repoId)).data;
  const rescanMutation = useRescanRepoMutation(repoId);

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

        <ClientOnly fallback={<RepoActionFallback />}>
          <RepoActions repo={repo} rescanMutation={rescanMutation} />
        </ClientOnly>
      </div>

      {rescanMutation.error ? (
        <ErrorCallout error={rescanMutation.error} title="Rescan failed" />
      ) : null}
      {repo.last_error ? (
        <Callout tone="warning" title="Repository reported an error">
          {repo.last_error}
        </Callout>
      ) : null}

      <div className="grid gap-4 md:grid-cols-4">
        <RepoMetric label="Provider" value={repo.provider} />
        <RepoMetric label="Provider host" value={repo.provider_host} />
        <RepoMetric label="Default branch" value={repo.default_branch} />
        <RepoMetric label="Last scanned SHA" value={shortSHA(repo.last_scanned_sha)} />
        <RepoMetric label="Compatibility" value={repo.compatibility_status || "--"} />
        <RepoMetric label="Updated" value={formatDateTimeUTC(repo.updated_at)} />
      </div>

      <RepoCompatibilityPanel summary={repo.compatibility_summary} />

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Repo Contract</h2>
        <div className="space-y-2 rounded-lg border border-border p-4 text-sm text-muted-foreground">
          <p>Repo import stores ownership metadata and verifies clone access.</p>
          <p>Executions run directly from submitted commands.</p>
        </div>
      </section>
    </div>
  );
}

function RepoActions({
  repo,
  rescanMutation,
}: {
  repo: Repo;
  rescanMutation: ReturnType<typeof useRescanRepoMutation>;
}) {
  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        onClick={() => rescanMutation.mutate()}
        disabled={rescanMutation.isPending || !canRefresh(repo)}
        className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent disabled:opacity-50"
      >
        {rescanMutation.isPending ? "Rescanning..." : "Rescan"}
      </button>
    </div>
  );
}

function RepoActionFallback() {
  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        disabled
        className="rounded-md border border-border px-3 py-2 text-sm disabled:opacity-50"
      >
        Rescan
      </button>
    </div>
  );
}
