import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { RepoListEmptyState, RepoListItem } from "~/features/repos/components";
import { loadReposIndex, reposQuery } from "~/features/repos/queries";

export const Route = createFileRoute("/_authenticated/repos/")({
  loader: ({ context }) => loadReposIndex(context.queryClient, context.auth),
  component: ReposPage,
});

function ReposPage() {
  const auth = useSignedInAuth();
  const repos = useSuspenseQuery(reposQuery(auth)).data;

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
          {repos.map((repo) => (
            <RepoListItem key={repo.repo_id} repo={repo} />
          ))}
        </div>
      )}
    </div>
  );
}
