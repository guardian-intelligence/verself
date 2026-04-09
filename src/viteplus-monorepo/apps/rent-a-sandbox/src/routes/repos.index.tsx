import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { fetchRepos } from "~/lib/api";
import { keys } from "~/lib/query-keys";

export const Route = createFileRoute("/repos/")({
  component: ReposPage,
});

function ReposPage() {
  const { data: repos, isPending, error } = useQuery({
    queryKey: keys.repos(),
    queryFn: fetchRepos,
    refetchInterval: (query) => {
      const current = query.state.data;
      if (!current) return false;
      return current.some((repo) => isRepoRefreshing(repo.state)) ? 2_000 : false;
    },
  });

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

      {error && (
        <div className="border border-destructive/50 bg-destructive/5 rounded-lg p-4 text-sm text-destructive">
          Failed to load repos: {error.message}
        </div>
      )}

      {isPending ? (
        <div className="border border-border rounded-lg p-8 text-center text-muted-foreground">
          Loading repos...
        </div>
      ) : null}

      {!isPending && repos?.length === 0 ? (
        <div className="border border-border rounded-lg p-8 text-center space-y-3">
          <p className="text-muted-foreground">
            No repos imported yet. Start by importing a repo that uses
            <code className="mx-1">runs-on: forge-metal</code>.
          </p>
          <div>
            <Link
              to="/repos/new"
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
            >
              Import Your First Repo
            </Link>
          </div>
        </div>
      ) : null}

      {repos && repos.length > 0 ? (
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
                  <RepoMetric label="Active golden" value={shortID(repo.active_golden_generation_id)} />
                  <RepoMetric label="Last ready" value={shortSHA(repo.last_ready_sha)} />
                </div>

                {(repo.last_error || issues.length > 0) && (
                  <div className="mt-4 rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
                    {repo.last_error ? repo.last_error : `${issues.length} workflow issue(s) need attention`}
                  </div>
                )}
              </Link>
            );
          })}
        </div>
      ) : null}
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

function RepoStateBadge({ state }: { state: string }) {
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
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[state] ?? "bg-muted text-muted-foreground"}`}
    >
      {state.replaceAll("_", " ")}
    </span>
  );
}

function isRepoRefreshing(state: string): boolean {
  return state === "importing" || state === "waiting_for_bootstrap" || state === "preparing";
}

function shortSHA(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 12);
}

function shortID(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 8);
}
