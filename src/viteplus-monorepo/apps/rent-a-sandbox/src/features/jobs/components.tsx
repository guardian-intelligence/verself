import { useForm } from "@tanstack/react-form";
import { useSuspenseQuery } from "@tanstack/react-query";
import { ClientOnly, Link } from "@tanstack/react-router";
import { useStickToBottom } from "use-stick-to-bottom";
import { Callout } from "~/components/callout";
import { EmptyState } from "~/components/empty-state";
import { ErrorCallout } from "~/components/error-callout";
import { type ElectricExecution } from "~/lib/collections";
import { formatExecutionRepo, useExecutionLogs, useExecutionRows } from "./live";
import { executionQuery } from "./queries";
import { ExecutionStatusBadge, isExecutionActiveStatus } from "./status";
import {
  DEFAULT_EXECUTION_REF,
  validateExecutionRef,
  validateExecutionRepoUrl,
} from "./validation";
import { useCreateExecutionMutation, type CreateExecutionResult } from "./mutations";

export function ExecutionListPanel({ orgId }: { orgId: string }) {
  return (
    <ClientOnly fallback={<ExecutionListLoading />}>
      <ExecutionListPanelContent orgId={orgId} />
    </ClientOnly>
  );
}

function ExecutionListPanelContent({ orgId }: { orgId: string }) {
  const rows = useExecutionRows(orgId);

  if (rows.isLoading || rows.isIdle) {
    return <ExecutionListLoading />;
  }

  if (rows.isError) {
    return <ExecutionListError status={rows.status} />;
  }

  if (rows.isEmpty) {
    return <ExecutionListEmpty />;
  }

  return <ExecutionTable executions={rows.executions} />;
}

export function ExecutionDetailPanel({ jobId }: { jobId: string }) {
  const execution = useSuspenseQuery(executionQuery(jobId)).data;

  const attempt = execution.latest_attempt;
  const isRunning = isExecutionActiveStatus(execution.status);

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/jobs" className="text-muted-foreground hover:text-foreground text-sm">
          Executions
        </Link>
        <span className="text-muted-foreground">/</span>
        <h1 className="font-mono text-xl font-bold">{execution.execution_id.slice(0, 8)}</h1>
        <ExecutionStatusBadge status={execution.status} />
        {execution.repo_id && (
          <>
            <span className="text-muted-foreground">/</span>
            <Link
              to="/repos/$repoId"
              params={{ repoId: execution.repo_id }}
              className="text-sm text-primary hover:underline"
            >
              Repo
            </Link>
          </>
        )}
      </div>

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <InfoCard
          label="Repository"
          value={formatExecutionRepo(execution.repo, execution.repo_url)}
        />
        <InfoCard label="Ref" value={execution.ref || execution.default_branch || "--"} />
        <InfoCard
          label="Duration"
          value={attempt.duration_ms ? `${(attempt.duration_ms / 1000).toFixed(1)}s` : "--"}
        />
        <InfoCard label="Exit Code" value={String(attempt.exit_code ?? "--")} />
        <InfoCard
          label="ZFS Written"
          value={attempt.zfs_written ? formatBytes(attempt.zfs_written) : "--"}
        />
        <InfoCard label="Commit" value={execution.commit_sha || "--"} />
        <InfoCard label="Kind" value={execution.kind} />
        <InfoCard label="Attempt" value={attempt.attempt_id.slice(0, 8)} />
      </div>

      <ClientOnly fallback={<ExecutionLogsLoading isRunning={isRunning} />}>
        <ExecutionLogsPanel attemptId={attempt.attempt_id} isRunning={isRunning} />
      </ClientOnly>
    </div>
  );
}

export function ExecutionSubmissionForm({
  onSuccess,
}: {
  onSuccess: (execution: CreateExecutionResult) => void | Promise<void>;
}) {
  const mutation = useCreateExecutionMutation({ onSuccess });
  const form = useForm({
    defaultValues: {
      repoUrl: "",
      ref: DEFAULT_EXECUTION_REF,
    },
    onSubmit: async ({ value }) => {
      mutation.reset();
      await mutation.mutateAsync({
        repo_url: value.repoUrl,
        ref: value.ref,
      });
    },
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        e.stopPropagation();
        void form.handleSubmit();
      }}
      className="max-w-md space-y-4"
    >
      <form.Field
        name="repoUrl"
        validators={{
          onChange: ({ value }) => validateExecutionRepoUrl(value),
        }}
      >
        {(field) => (
          <div>
            <label htmlFor={field.name} className="text-sm font-medium">
              Repository URL
            </label>
            <input
              id={field.name}
              type="text"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="https://git.example.com/acme/repo.git"
              className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
            ) : null}
          </div>
        )}
      </form.Field>

      <form.Field
        name="ref"
        validators={{
          onChange: ({ value }) => validateExecutionRef(value),
        }}
      >
        {(field) => (
          <div>
            <label htmlFor={field.name} className="text-sm font-medium">
              Ref
            </label>
            <input
              id={field.name}
              type="text"
              value={field.state.value}
              onBlur={field.handleBlur}
              onChange={(e) => field.handleChange(e.target.value)}
              placeholder="refs/heads/main"
              className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono"
            />
            {field.state.meta.isTouched && field.state.meta.errors.length > 0 ? (
              <p className="mt-1 text-sm text-destructive">{field.state.meta.errors[0]}</p>
            ) : null}
          </div>
        )}
      </form.Field>

      {mutation.error ? (
        <ErrorCallout title="Execution submission failed" error={mutation.error} />
      ) : null}

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
        {([canSubmit, isSubmitting]) => (
          <button
            type="submit"
            disabled={!canSubmit || isSubmitting || mutation.isPending}
            className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
          >
            {mutation.isPending || isSubmitting ? "Submitting..." : "Submit Execution"}
          </button>
        )}
      </form.Subscribe>
    </form>
  );
}

function ExecutionLogsPanel({ attemptId, isRunning }: { attemptId: string; isRunning: boolean }) {
  const logs = useExecutionLogs(attemptId);

  if (logs.isLoading || logs.isIdle) {
    return <ExecutionLogsLoading isRunning={isRunning} />;
  }

  if (logs.isError) {
    return <ExecutionLogsError status={logs.status} isRunning={isRunning} />;
  }

  if (logs.isEmpty) {
    return <ExecutionLogsEmpty isRunning={isRunning} />;
  }

  return <ExecutionLogsBody logText={logs.logText} />;
}

function ExecutionTable({ executions }: { executions: ElectricExecution[] }) {
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50">
          <tr>
            <th className="px-4 py-2 text-left font-medium">ID</th>
            <th className="px-4 py-2 text-left font-medium">Repository</th>
            <th className="px-4 py-2 text-left font-medium">Ref</th>
            <th className="px-4 py-2 text-left font-medium">Status</th>
            <th className="px-4 py-2 text-left font-medium">Created</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {executions.map((execution) => (
            <tr key={execution.execution_id} className="hover:bg-accent/30">
              <td className="px-4 py-2">
                <Link
                  to="/jobs/$jobId"
                  params={{ jobId: execution.execution_id }}
                  className="font-mono text-primary hover:underline"
                >
                  {execution.execution_id.slice(0, 8)}
                </Link>
              </td>
              <td className="max-w-[300px] truncate px-4 py-2">
                {formatExecutionRepo(execution.repo, execution.repo_url)}
              </td>
              <td className="px-4 py-2 font-mono">
                {execution.ref || execution.default_branch || "--"}
              </td>
              <td className="px-4 py-2">
                <ExecutionStatusBadge status={execution.status} />
              </td>
              <td className="px-4 py-2 text-muted-foreground">
                {new Date(execution.created_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ExecutionListLoading() {
  return <EmptyState title="Loading executions" body="Synchronizing the latest execution state." />;
}

function ExecutionListError({ status }: { status: string }) {
  return (
    <ErrorCallout
      title="Could not load executions"
      error={`Execution sync failed (${status}).`}
      action={
        <Link
          to="/jobs"
          className="rounded-md border border-border px-3 py-1.5 text-foreground hover:bg-accent"
        >
          Retry
        </Link>
      }
    />
  );
}

function ExecutionListEmpty() {
  return (
    <EmptyState
      title="No executions yet"
      body="Import a repo or launch a manual execution to get started."
      action={
        <div className="flex flex-wrap items-center justify-center gap-3">
          <Link
            to="/repos"
            className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-accent"
          >
            Import a repo
          </Link>
          <Link
            to="/jobs/new"
            className="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:opacity-90"
          >
            Manual execution
          </Link>
        </div>
      }
    />
  );
}

function ExecutionLogsLoading({ isRunning }: { isRunning: boolean }) {
  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">Logs</h2>
      <Callout title="Loading logs">
        {isRunning ? "Waiting for output..." : "No log output yet."}
      </Callout>
    </div>
  );
}

function ExecutionLogsError({ status, isRunning }: { status: string; isRunning: boolean }) {
  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">Logs</h2>
      <ErrorCallout
        title="Log stream unavailable"
        error={`Log stream unavailable (${status}).`}
        action={
          <div className="text-xs text-muted-foreground">
            {isRunning
              ? "The attempt is still running, but logs have not synced yet."
              : "No log output is available."}
          </div>
        }
      />
    </div>
  );
}

function ExecutionLogsEmpty({ isRunning }: { isRunning: boolean }) {
  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">Logs</h2>
      <Callout title="Logs">{isRunning ? "Waiting for output..." : "No log output."}</Callout>
    </div>
  );
}

function ExecutionLogsBody({ logText }: { logText: string }) {
  const { scrollRef, contentRef, isAtBottom, scrollToBottom } = useStickToBottom();

  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">Logs</h2>
      <div className="relative">
        <pre
          ref={scrollRef}
          className="max-h-[600px] overflow-x-auto overflow-y-auto whitespace-pre-wrap rounded-lg border border-border bg-foreground/5 p-4 font-mono text-sm"
        >
          <div ref={contentRef}>{logText}</div>
        </pre>
        {!isAtBottom && logText ? (
          <button
            onClick={() => scrollToBottom()}
            className="absolute bottom-3 right-3 rounded-md bg-primary px-3 py-1.5 text-xs text-primary-foreground shadow-md hover:opacity-90"
          >
            Scroll to bottom
          </button>
        ) : null}
      </div>
    </div>
  );
}

function InfoCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-border p-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-medium">{value}</div>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
