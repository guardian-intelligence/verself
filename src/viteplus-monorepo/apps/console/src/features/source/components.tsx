import { useSuspenseQuery } from "@tanstack/react-query";
import { useSignedInAuth } from "@verself/auth-web/react";
import { GitBranch } from "lucide-react";
import { EmptyState } from "~/components/empty-state";
import type { SourceRepository } from "~/server-fns/api";
import { sourceRepositoriesQuery, sourceWorkflowRunsQuery } from "./queries";

export function BuildRepositoriesPanel() {
  const auth = useSignedInAuth();
  const { repositories } = useSuspenseQuery(sourceRepositoriesQuery(auth)).data;

  if (repositories.length === 0) {
    return (
      <EmptyState
        icon={<GitBranch className="size-5" />}
        title="No repositories"
        body="Add a repository to run builds."
      />
    );
  }

  return (
    <div className="grid gap-2">
      {repositories.map((repo) => (
        <BuildRepositoryRow key={repo.repo_id} repo={repo} />
      ))}
    </div>
  );
}

function BuildRepositoryRow({ repo }: { repo: SourceRepository }) {
  const auth = useSignedInAuth();
  const runs = useSuspenseQuery(sourceWorkflowRunsQuery(auth, repo.repo_id)).data.workflow_runs;
  const activeBuilds = runs.filter(isActiveBuild).length;

  return (
    <div
      className="flex min-h-12 items-center justify-between gap-4 rounded-md border bg-card px-4 py-3"
      data-testid="build-repository-row"
    >
      <span className="min-w-0 truncate font-mono text-sm" data-testid="build-repository-slug">
        {repo.org_slug}/{repo.project_slug}
      </span>
      <span
        className="shrink-0 text-sm font-medium text-muted-foreground tabular-nums"
        data-testid="build-active-count"
      >
        {activeBuilds} {activeBuilds === 1 ? "active build" : "active builds"}
      </span>
    </div>
  );
}

function isActiveBuild(run: { readonly state: string }) {
  return run.state === "dispatching" || run.state === "dispatched";
}
