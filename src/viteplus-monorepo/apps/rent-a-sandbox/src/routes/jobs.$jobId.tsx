import { ClientOnly, createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useLiveQuery } from "@tanstack/react-db";
import { useStickToBottom } from "use-stick-to-bottom";
import { useMemo } from "react";
import { getExecution } from "~/server-fns/api";
import { createExecutionLogsCollection } from "~/lib/collections";
import { keys } from "~/lib/query-keys";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/jobs/$jobId")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ params }) => getExecution({ data: { executionId: params.jobId } }),
  component: JobDetailPage,
});

function JobDetailPage() {
  const { jobId } = Route.useParams();
  const initialExecution = Route.useLoaderData();

  const { data: execution, error } = useQuery({
    queryKey: keys.job(jobId),
    queryFn: () => getExecution({ data: { executionId: jobId } }),
    initialData: initialExecution,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return isActiveStatus(status) ? 2_000 : false;
    },
  });

  if (error) {
    return (
      <div className="py-12 text-center">
        <p className="text-destructive">Failed to load sandbox: {error.message}</p>
      </div>
    );
  }

  if (!execution) {
    return <div className="py-12 text-center text-muted-foreground">Loading...</div>;
  }

  const attempt = execution.latest_attempt;

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/jobs" className="text-muted-foreground hover:text-foreground text-sm">
          Executions
        </Link>
        <span className="text-muted-foreground">/</span>
        <h1 className="text-xl font-bold font-mono">{execution.execution_id.slice(0, 8)}</h1>
        <StatusBadge status={execution.status} />
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

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <InfoCard label="Repository" value={displayRepo(execution.repo, execution.repo_url)} />
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

      <ClientOnly fallback={<ExecutionLogsLoading isRunning={isActiveStatus(execution.status)} />}>
        <LiveExecutionLogs
          attemptId={attempt.attempt_id}
          isRunning={isActiveStatus(execution.status)}
        />
      </ClientOnly>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    queued: "bg-yellow-100 text-yellow-800",
    reserved: "bg-amber-100 text-amber-800",
    launching: "bg-sky-100 text-sky-800",
    running: "bg-blue-100 text-blue-800",
    finalizing: "bg-indigo-100 text-indigo-800",
    succeeded: "bg-green-100 text-green-800",
    failed: "bg-red-100 text-red-800",
    canceled: "bg-zinc-200 text-zinc-800",
    lost: "bg-fuchsia-100 text-fuchsia-800",
  };
  return (
    <span
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[status] ?? "bg-muted text-muted-foreground"}`}
    >
      {status}
    </span>
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

function LiveExecutionLogs({
  attemptId,
  isRunning,
}: {
  attemptId: string;
  isRunning: boolean;
}) {
  const { scrollRef, contentRef, isAtBottom, scrollToBottom } = useStickToBottom();
  const collection = useMemo(() => createExecutionLogsCollection(attemptId), [attemptId]);
  const { data: logChunks } = useLiveQuery((q) => q.from({ l: collection }), [collection]);
  const logText = useMemo(() => {
    if (!logChunks || logChunks.length === 0) return "";
    return [...logChunks]
      .sort((a, b) => a.seq - b.seq)
      .map((chunk) => chunk.chunk)
      .join("");
  }, [logChunks]);

  return (
    <div>
      <h2 className="text-lg font-semibold mb-2">Logs</h2>
      <div className="relative">
        <pre
          ref={scrollRef}
          className="bg-foreground/5 border border-border rounded-lg p-4 text-sm font-mono overflow-x-auto max-h-[600px] overflow-y-auto whitespace-pre-wrap"
        >
          <div ref={contentRef}>
            {logText || (isRunning ? "Waiting for output..." : "No log output.")}
          </div>
        </pre>
        {!isAtBottom && logText && (
          <button
            onClick={() => scrollToBottom()}
            className="absolute bottom-3 right-3 px-3 py-1.5 rounded-md bg-primary text-primary-foreground text-xs hover:opacity-90 shadow-md"
          >
            Scroll to bottom
          </button>
        )}
      </div>
    </div>
  );
}

function ExecutionLogsLoading({ isRunning }: { isRunning: boolean }) {
  return (
    <div>
      <h2 className="text-lg font-semibold mb-2">Logs</h2>
      <div className="relative">
        <pre className="bg-foreground/5 border border-border rounded-lg p-4 text-sm font-mono overflow-x-auto max-h-[600px] overflow-y-auto whitespace-pre-wrap">
          {isRunning ? "Waiting for output..." : "No log output."}
        </pre>
      </div>
    </div>
  );
}

function displayRepo(repo?: string, repoURL?: string): string {
  if (repo) return repo;
  if (!repoURL) return "--";
  return repoURL.replace("https://", "");
}

function isActiveStatus(status?: string): boolean {
  return (
    status === "queued" ||
    status === "reserved" ||
    status === "launching" ||
    status === "running" ||
    status === "finalizing"
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
