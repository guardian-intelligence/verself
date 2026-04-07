import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useLiveQuery } from "@tanstack/react-db";
import { useMemo, useEffect, useRef } from "react";
import { fetchJob, type Job } from "~/lib/api";
import { createJobLogsCollection, type ElectricJobLog } from "~/lib/collections";

export const Route = createFileRoute("/jobs/$jobId")({
  component: JobDetailPage,
});

function JobDetailPage() {
  const { jobId } = Route.useParams();

  const { data: job, error } = useQuery({
    queryKey: ["jobs", jobId],
    queryFn: () => fetchJob(jobId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "running" ? 2_000 : false;
    },
  });

  if (error) {
    return (
      <div className="py-12 text-center">
        <p className="text-destructive">Failed to load job: {error.message}</p>
      </div>
    );
  }

  if (!job) {
    return (
      <div className="py-12 text-center text-muted-foreground">Loading...</div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link
          to="/jobs"
          className="text-muted-foreground hover:text-foreground text-sm"
        >
          Sandboxes
        </Link>
        <span className="text-muted-foreground">/</span>
        <h1 className="text-xl font-bold font-mono">{job.id.slice(0, 8)}</h1>
        <StatusBadge status={job.status} />
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <InfoCard
          label="Repository"
          value={job.repo_url.replace("https://", "")}
        />
        <InfoCard
          label="Duration"
          value={
            job.duration_ms
              ? `${(job.duration_ms / 1000).toFixed(1)}s`
              : "--"
          }
        />
        <InfoCard label="Exit Code" value={String(job.exit_code ?? "--")} />
        <InfoCard
          label="ZFS Written"
          value={job.zfs_written ? formatBytes(job.zfs_written) : "--"}
        />
      </div>

      <LiveJobLogs jobId={jobId} isRunning={job.status === "running"} />
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    running: "bg-blue-100 text-blue-800",
    completed: "bg-green-100 text-green-800",
    failed: "bg-red-100 text-red-800",
    pending: "bg-yellow-100 text-yellow-800",
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

/** Electric-backed live log viewer. Chunks stream in real-time via PG
 *  replication → Electric → browser. Falls back to "waiting" message. */
function LiveJobLogs({
  jobId,
  isRunning,
}: {
  jobId: string;
  isRunning: boolean;
}) {
  const collection = useMemo(() => createJobLogsCollection(jobId), [jobId]);
  const logEndRef = useRef<HTMLDivElement>(null);

  const { data: logChunks } = useLiveQuery(
    (q) => q.from({ l: collection }),
    [collection],
  );

  // Auto-scroll to bottom when new logs arrive while running.
  useEffect(() => {
    if (isRunning && logEndRef.current) {
      logEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [logChunks?.length, isRunning]);

  // Sort chunks by sequence number before concatenating.
  const logText = logChunks
    ? [...logChunks]
        .sort((a, b) => Number(a.seq) - Number(b.seq))
        .map((c) => (typeof c.chunk === "string" ? c.chunk : ""))
        .join("")
    : "";

  return (
    <div>
      <h2 className="text-lg font-semibold mb-2">Logs</h2>
      <pre className="bg-foreground/5 border border-border rounded-lg p-4 text-sm font-mono overflow-x-auto max-h-[600px] overflow-y-auto whitespace-pre-wrap">
        {logText || (isRunning ? "Waiting for output..." : "No log output.")}
        <div ref={logEndRef} />
      </pre>
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
