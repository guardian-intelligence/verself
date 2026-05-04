import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@verself/auth-web/react";
import { GitBranch } from "lucide-react";
import { Callout } from "~/components/callout";
import { EmptyState } from "~/components/empty-state";
import { useExecutionRows, useRunnerProviderRepositoryRows } from "~/features/executions/live";
import { ExecutionStatusBadge, isExecutionActiveStatus } from "~/features/executions/status";
import type { ElectricExecution, ElectricRunnerProviderRepository } from "~/lib/collections";
import { formatDateTimeUTC } from "~/lib/format";
import type { SourceRepository } from "~/server-fns/api";
import { sourceRepositoriesQuery } from "./queries";

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

  if (!auth.orgId) {
    return (
      <Callout tone="destructive" title="Missing organization">
        Your session is missing organization context. Try signing out and back in.
      </Callout>
    );
  }

  return (
    <ClientOnly fallback={<BuildsLiveLoading />}>
      <BuildRepositoryRowsLive repositories={repositories} />
    </ClientOnly>
  );
}

export function BuildRepositoryActiveBuildsPanel({ repo }: { repo: SourceRepository }) {
  const auth = useSignedInAuth();

  if (!auth.orgId) {
    return (
      <Callout tone="destructive" title="Missing organization">
        Your session is missing organization context. Try signing out and back in.
      </Callout>
    );
  }

  return (
    <ClientOnly fallback={<BuildsLiveLoading />}>
      <BuildRepositoryActiveBuildsLive repo={repo} />
    </ClientOnly>
  );
}

export function buildRepositorySlug(repo: SourceRepository) {
  return `${repo.org_slug}/${repo.project_slug}`;
}

function BuildRepositoryRowsLive({ repositories }: { repositories: SourceRepository[] }) {
  const executionRows = useExecutionRows();
  const providerRepositoryRows = useRunnerProviderRepositoryRows();

  if (
    executionRows.isLoading ||
    executionRows.isIdle ||
    providerRepositoryRows.isLoading ||
    providerRepositoryRows.isIdle
  ) {
    return <BuildsLiveLoading />;
  }

  if (executionRows.isError || providerRepositoryRows.isError) {
    return (
      <BuildsLiveError
        status={executionRows.isError ? executionRows.status : providerRepositoryRows.status}
      />
    );
  }

  return (
    <div className="grid gap-2">
      {repositories.map((repo) => (
        <BuildRepositoryRow
          key={repo.repo_id}
          repo={repo}
          executions={executionRows.executions}
          providerRepositories={providerRepositoryRows.repositories}
        />
      ))}
    </div>
  );
}

function BuildRepositoryRow({
  repo,
  executions,
  providerRepositories,
}: {
  repo: SourceRepository;
  executions: ElectricExecution[];
  providerRepositories: ElectricRunnerProviderRepository[];
}) {
  const repoSlug = buildRepositorySlug(repo);
  const activeBuilds = activeBuildsForRepository(repo, executions, providerRepositories);

  return (
    <div
      className="flex min-h-12 items-center justify-between gap-4 rounded-md border bg-card px-4 py-3"
      data-testid="build-repository-row"
      data-repo-id={repo.repo_id}
      data-repo-slug={repoSlug}
      data-active-build-count={activeBuilds.length}
    >
      <span className="min-w-0 truncate font-mono text-sm" data-testid="build-repository-slug">
        {repoSlug}
      </span>
      <ActiveBuildCountLink repo={repo} activeBuilds={activeBuilds} />
    </div>
  );
}

function ActiveBuildCountLink({
  repo,
  activeBuilds,
}: {
  repo: SourceRepository;
  activeBuilds: ElectricExecution[];
}) {
  const label = `${activeBuilds.length} ${activeBuilds.length === 1 ? "active build" : "active builds"}`;
  const className = "shrink-0 text-sm font-medium tabular-nums";
  const onlyBuild = activeBuilds[0];

  if (activeBuilds.length === 1 && onlyBuild) {
    return (
      <Link
        to="/executions/$executionId"
        params={{ executionId: onlyBuild.execution_id }}
        className={`${className} text-foreground hover:underline`}
        data-testid="build-active-link"
      >
        {label}
      </Link>
    );
  }

  if (activeBuilds.length > 1) {
    return (
      <Link
        to="/builds/$repoId"
        params={{ repoId: repo.repo_id }}
        className={`${className} text-foreground hover:underline`}
        data-testid="build-active-link"
      >
        {label}
      </Link>
    );
  }

  return (
    <span className={`${className} text-muted-foreground`} data-testid="build-active-count">
      {label}
    </span>
  );
}

function BuildRepositoryActiveBuildsLive({ repo }: { repo: SourceRepository }) {
  const executionRows = useExecutionRows();
  const providerRepositoryRows = useRunnerProviderRepositoryRows();

  if (
    executionRows.isLoading ||
    executionRows.isIdle ||
    providerRepositoryRows.isLoading ||
    providerRepositoryRows.isIdle
  ) {
    return <BuildsLiveLoading />;
  }

  if (executionRows.isError || providerRepositoryRows.isError) {
    return (
      <BuildsLiveError
        status={executionRows.isError ? executionRows.status : providerRepositoryRows.status}
      />
    );
  }

  const activeBuilds = activeBuildsForRepository(
    repo,
    executionRows.executions,
    providerRepositoryRows.repositories,
  );

  if (activeBuilds.length === 0) {
    return <EmptyState title="No active builds" body="This repository has no running builds." />;
  }

  return (
    <div className="grid gap-2">
      {activeBuilds.map((execution) => (
        <Link
          key={execution.execution_id}
          to="/executions/$executionId"
          params={{ executionId: execution.execution_id }}
          className="flex min-h-12 items-center justify-between gap-4 rounded-md border bg-card px-4 py-3 hover:bg-muted/50"
          data-testid="repository-active-build-row"
        >
          <span className="min-w-0 truncate font-mono text-sm font-semibold">
            {execution.execution_id.slice(0, 8)}
          </span>
          <span className="flex shrink-0 items-center gap-3">
            <ExecutionStatusBadge status={execution.state} />
            <span className="hidden text-xs tabular-nums text-muted-foreground sm:inline">
              {formatDateTimeUTC(execution.created_at)}
            </span>
          </span>
        </Link>
      ))}
    </div>
  );
}

function activeBuildsForRepository(
  repo: SourceRepository,
  executions: readonly ElectricExecution[],
  providerRepositories: readonly ElectricRunnerProviderRepository[],
): ElectricExecution[] {
  const sourceRefs = sourceRefsForRepository(repo, providerRepositories);
  return executions.filter(
    (execution) => sourceRefs.has(execution.source_ref) && isExecutionActiveStatus(execution.state),
  );
}

function sourceRefsForRepository(
  repo: SourceRepository,
  providerRepositories: readonly ElectricRunnerProviderRepository[],
) {
  const sourceRefs = new Set([buildRepositorySlug(repo)]);
  for (const providerRepository of providerRepositories) {
    if (providerRepository.active && providerRepository.source_repository_id === repo.repo_id) {
      sourceRefs.add(providerRepository.repository_full_name);
    }
  }
  return sourceRefs;
}

function BuildsLiveLoading() {
  return <EmptyState title="Loading builds" body="Synchronizing execution state." />;
}

function BuildsLiveError({ status }: { status: string }) {
  return (
    <Callout tone="destructive" title="Could not load builds">
      Execution sync failed ({status}).
    </Callout>
  );
}
