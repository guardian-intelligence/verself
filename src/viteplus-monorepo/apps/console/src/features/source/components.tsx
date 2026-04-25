import { useSuspenseQuery } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { GitBranch, GitCommit, GitPullRequest, KeyRound, PlayCircle, Terminal } from "lucide-react";
import { EmptyState } from "~/components/empty-state";
import { formatDateTimeUTC } from "~/lib/format";
import type { SourceRepository, SourceWorkflowRunList } from "~/server-fns/api";
import { sourceRefsQuery, sourceRepositoriesQuery, sourceWorkflowRunsQuery } from "./queries";

const ACTIVE_WORKFLOW_STATES = new Set(["dispatching", "dispatched", "queued", "running"]);

export function SourceRepositoriesPanel({ gitOrigin }: { gitOrigin: string }) {
  const auth = useSignedInAuth();
  const { repositories } = useSuspenseQuery(sourceRepositoriesQuery(auth)).data;

  if (repositories.length === 0) {
    return <SourcePushEmptyState gitOrigin={gitOrigin} />;
  }

  return (
    <div className="grid gap-4">
      {repositories.map((repo) => (
        <SourceRepositoryCard key={repo.repo_id} repo={repo} />
      ))}
    </div>
  );
}

function SourceRepositoryCard({ repo }: { repo: SourceRepository }) {
  const auth = useSignedInAuth();
  const refs = useSuspenseQuery(sourceRefsQuery(auth, repo.repo_id)).data.refs;
  const workflowRuns = useSuspenseQuery(sourceWorkflowRunsQuery(auth, repo.repo_id)).data;
  const activeBranches = refs.filter((ref) => ref.name !== repo.default_branch);
  const runningJobs = activeWorkflowRuns(workflowRuns);

  return (
    <article className="rounded-md border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b px-4 py-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold leading-6 tracking-tight">{repo.name}</h2>
            <Badge variant="outline">{repo.provider}</Badge>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs font-medium text-muted-foreground">
            <span>{repo.slug}</span>
            <span className="font-mono">{repo.default_branch}</span>
            <span>{formatDateTimeUTC(repo.updated_at)}</span>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant={runningJobs.length > 0 ? "warning" : "outline"}>
            {runningJobs.length} running CI
          </Badge>
          <Badge variant={activeBranches.length > 0 ? "info" : "outline"}>
            {activeBranches.length} active branches
          </Badge>
        </div>
      </div>

      <div className="grid gap-0 md:grid-cols-2">
        <div className="border-b px-4 py-4 md:border-b-0 md:border-r">
          <div className="mb-3 flex items-center gap-2 text-sm font-semibold">
            <GitPullRequest className="size-4 text-muted-foreground" aria-hidden="true" />
            Branches
          </div>
          {activeBranches.length === 0 ? (
            <p className="text-sm text-muted-foreground">No active PR branches.</p>
          ) : (
            <ul className="grid gap-3">
              {activeBranches.map((branch) => {
                const branchJob = runningJobs.find((job) => job.ref === branch.name);
                return (
                  <li key={branch.name} className="flex min-w-0 items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate font-mono text-sm">{branch.name}</div>
                      <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                        <GitCommit className="size-3" aria-hidden="true" />
                        <span className="font-mono">{shortCommit(branch.commit)}</span>
                      </div>
                    </div>
                    <Badge variant={branchJob ? workflowBadgeVariant(branchJob.state) : "outline"}>
                      {branchJob ? branchJob.state : "no CI"}
                    </Badge>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className="px-4 py-4">
          <div className="mb-3 flex items-center gap-2 text-sm font-semibold">
            <PlayCircle className="size-4 text-muted-foreground" aria-hidden="true" />
            CI jobs
          </div>
          {runningJobs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No running CI jobs.</p>
          ) : (
            <ul className="grid gap-3">
              {runningJobs.map((job) => (
                <li key={job.workflow_run_id} className="min-w-0">
                  <div className="flex min-w-0 items-center justify-between gap-3">
                    <span className="truncate font-mono text-sm">{job.ref}</span>
                    <Badge variant={workflowBadgeVariant(job.state)}>{job.state}</Badge>
                  </div>
                  <div className="mt-1 truncate text-xs text-muted-foreground">
                    {job.workflow_path} · {formatDateTimeUTC(job.updated_at)}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </article>
  );
}

function SourcePushEmptyState({ gitOrigin }: { gitOrigin: string }) {
  const pushURL = `${gitOrigin.replace(/\/$/, "")}/<project>.git`;

  return (
    <EmptyState
      icon={<GitBranch className="size-5" aria-hidden="true" />}
      title="Push the first branch"
      body={
        <div className="grid gap-4 text-left">
          <p>
            A project gets one hosted repository. The first authenticated push creates that
            repository; later branch pushes become the active work queue.
          </p>
          <div className="grid gap-2 rounded-md border bg-background p-3">
            <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
              <Terminal className="size-3.5" aria-hidden="true" />
              Git remote
            </div>
            <code className="break-all font-mono text-xs text-foreground">
              git remote add forge {pushURL}
            </code>
            <code className="break-all font-mono text-xs text-foreground">git push forge main</code>
          </div>
          <div className="grid gap-2 rounded-md border bg-background p-3">
            <div className="flex items-center gap-2 text-xs font-semibold text-muted-foreground">
              <KeyRound className="size-3.5" aria-hidden="true" />
              Authentication
            </div>
            <p className="text-xs">
              Sign in through Zitadel on the Git origin, add an SSH key or mint a scoped Git
              credential, then push over SSH or HTTPS.
            </p>
          </div>
        </div>
      }
    />
  );
}

function activeWorkflowRuns(workflowRuns: SourceWorkflowRunList) {
  return workflowRuns.workflow_runs.filter((run) => ACTIVE_WORKFLOW_STATES.has(run.state));
}

function workflowBadgeVariant(state: string) {
  switch (state) {
    case "running":
    case "dispatched":
      return "info";
    case "dispatching":
    case "queued":
      return "warning";
    case "failed":
      return "destructive";
    default:
      return "outline";
  }
}

function shortCommit(commit: string) {
  return commit.length > 12 ? commit.slice(0, 12) : commit;
}
