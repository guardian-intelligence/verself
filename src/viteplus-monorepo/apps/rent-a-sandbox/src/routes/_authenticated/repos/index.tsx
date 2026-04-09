import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Suspense } from "react";
import { RepoStateBadge, shortID, shortSHA } from "~/components/repo-state";
import { RepoListEmptyState, RepoListLoadingState } from "~/features/repos/components";
import { loadReposIndex, reposQuery } from "~/features/repos/queries";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/repos/")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context }) => loadReposIndex(context.queryClient),
  component: ReposPage,
});

function ReposPage() {
  return (
    <Suspense fallback={<RepoListLoadingState />}>
      <ReposPageContent />
    </Suspense>
  );
}

function ReposPageContent() {
  const { data: repos } = useSuspenseQuery(reposQuery());

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-bold">Repos</h1>
          <p className="text-sm text-muted-foreground">
            Import a repository, validate its workflow labels, and prepare its active golden.
          </p>
        </div>
        <Link
          to="/repos/new"
          className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
        >
          Import Repo
        </Link>
      </div>

      {repos.length === 0 ? (
        <RepoListEmptyState />
      ) : (
        <div className="grid gap-4">
          {repos.map((repo) => {
            const issues = repo.compatibility_summary?.issues ?? [];
            return (
              <Link
                key={repo.repo_id}
                to="/repos/$repoId"
                params={{ repoId: repo.repo_id }}
                className="border border-border rounded-lg p-5 hover:bg-accent/30 transition-colors"
              >
                <div className="flex items-start justify-between gap-4">
                  <div className="space-y-2 min-w-0">
                    <div className="flex items-center gap-3 flex-wrap">
                      <h2 className="font-semibold text-lg truncate">{repo.full_name}</h2>
                      <RepoStateBadge state={repo.state} />
                    </div>
                    <p className="text-sm text-muted-foreground truncate">{repo.clone_url}</p>
                  </div>
                  <div className="text-right text-sm text-muted-foreground">
                    <div>Profile: {repo.runner_profile_slug}</div>
                    <div>Default branch: {repo.default_branch}</div>
                  </div>
                </div>

                <div className="grid md:grid-cols-4 gap-3 mt-4 text-sm">
                  <RepoMetric
                    label="Compatibility"
                    value={
                      repo.compatibility_status === "compatible"
                        ? "Compatible"
                        : repo.compatibility_status || "--"
                    }
                  />
                  <RepoMetric label="Last scanned" value={shortSHA(repo.last_scanned_sha)} />
                  <RepoMetric
                    label="Active golden"
                    value={shortID(repo.active_golden_generation_id)}
                  />
                  <RepoMetric label="Last ready" value={shortSHA(repo.last_ready_sha)} />
                </div>

                {(repo.last_error || issues.length > 0) && (
                  <div className="mt-4 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
                    {repo.last_error
                      ? repo.last_error
                      : `${issues.length} workflow issue(s) need attention`}
                  </div>
                )}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

function RepoMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-medium truncate">{value}</div>
    </div>
  );
}
