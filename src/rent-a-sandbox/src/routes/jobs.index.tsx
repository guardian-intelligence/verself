import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { fetchBalance } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { useLiveQuery } from "@tanstack/react-db";
import {
  createJobsCollection,
  type ElectricJob,
} from "~/lib/collections";
import { useMemo } from "react";

export const Route = createFileRoute("/jobs/")({
  component: JobsPage,
});

function JobsPage() {
  const { data: balance } = useQuery({
    queryKey: keys.balance(),
    queryFn: fetchBalance,
    staleTime: 5_000,
  });

  const creditsExhausted = balance ? balance.total_available <= 0 : false;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Sandboxes</h1>
        {creditsExhausted ? (
          <span
            title="No credits remaining — purchase more at /billing/credits"
            className="px-4 py-2 rounded-md bg-muted text-muted-foreground text-sm cursor-not-allowed"
          >
            New Sandbox
          </span>
        ) : (
          <Link
            to="/jobs/new"
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
          >
            New Sandbox
          </Link>
        )}
      </div>

      {creditsExhausted && (
        <div className="border border-destructive/50 bg-destructive/5 rounded-lg p-4 text-sm flex items-center justify-between">
          <span>
            Your credit balance is empty. Purchase credits to create sandboxes.
          </span>
          <Link
            to="/billing/credits"
            className="px-3 py-1.5 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm whitespace-nowrap"
          >
            Buy Credits
          </Link>
        </div>
      )}

      {balance?.org_id ? (
        <LiveJobTable orgId={balance.org_id} />
      ) : (
        <JobTableSkeleton />
      )}
    </div>
  );
}

/** Live job table backed by Electric real-time sync.
 *  Only mounts once we have org_id (i.e. after auth + balance load),
 *  which naturally prevents SSR rendering. */
function LiveJobTable({ orgId }: { orgId: string }) {
  const collection = useMemo(() => createJobsCollection(orgId), [orgId]);

  const { data: jobs } = useLiveQuery(
    (q) => q.from({ j: collection }),
    [collection],
  );

  // Sort by created_at descending (most recent first).
  const sortedJobs = useMemo(
    () =>
      jobs
        ? [...jobs].sort(
            (a, b) =>
              new Date(b.created_at).getTime() -
              new Date(a.created_at).getTime(),
          )
        : null,
    [jobs],
  );

  if (!sortedJobs || sortedJobs.length === 0) {
    return (
      <div className="border border-border rounded-lg p-8 text-center text-muted-foreground">
        No sandboxes yet. Create one to get started.
      </div>
    );
  }

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <table className="w-full text-sm">
        <thead className="bg-muted/50">
          <tr>
            <th className="text-left px-4 py-2 font-medium">ID</th>
            <th className="text-left px-4 py-2 font-medium">Repository</th>
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Duration</th>
            <th className="text-left px-4 py-2 font-medium">Created</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {sortedJobs.map((job) => (
            <tr key={job.id} className="hover:bg-accent/30">
              <td className="px-4 py-2">
                <Link
                  to="/jobs/$jobId"
                  params={{ jobId: job.id }}
                  className="font-mono text-primary hover:underline"
                >
                  {job.id.slice(0, 8)}
                </Link>
              </td>
              <td className="px-4 py-2 truncate max-w-[300px]">
                {job.repo_url.replace("https://", "")}
              </td>
              <td className="px-4 py-2">
                <StatusBadge status={job.status} />
              </td>
              <td className="px-4 py-2 font-mono">
                {job.duration_ms
                  ? `${(Number(job.duration_ms) / 1000).toFixed(1)}s`
                  : "--"}
              </td>
              <td className="px-4 py-2 text-muted-foreground">
                {new Date(job.created_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
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

function JobTableSkeleton() {
  return (
    <div className="border border-border rounded-lg p-8 text-center text-muted-foreground">
      Sign in to view your sandboxes.
    </div>
  );
}
