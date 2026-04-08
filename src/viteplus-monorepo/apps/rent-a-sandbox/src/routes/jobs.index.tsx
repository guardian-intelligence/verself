import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { fetchBalance } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { useLiveQuery } from "@tanstack/react-db";
import { createExecutionsCollection } from "~/lib/collections";
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
          <span>Your credit balance is empty. Purchase credits to create sandboxes.</span>
          <Link
            to="/billing/credits"
            className="px-3 py-1.5 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm whitespace-nowrap"
          >
            Buy Credits
          </Link>
        </div>
      )}

      {balance?.org_id ? <LiveExecutionTable orgId={balance.org_id} /> : <ExecutionTableSkeleton />}
    </div>
  );
}

function LiveExecutionTable({ orgId }: { orgId: string }) {
  const collection = useMemo(() => createExecutionsCollection(orgId), [orgId]);

  const { data: executions } = useLiveQuery((q) => q.from({ e: collection }), [collection]);

  const sortedExecutions = useMemo(
    () =>
      executions
        ? [...executions].sort(
            (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
          )
        : null,
    [executions],
  );

  if (!sortedExecutions || sortedExecutions.length === 0) {
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
            <th className="text-left px-4 py-2 font-medium">Ref</th>
            <th className="text-left px-4 py-2 font-medium">Status</th>
            <th className="text-left px-4 py-2 font-medium">Created</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {sortedExecutions.map((execution) => (
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
              <td className="px-4 py-2 truncate max-w-[300px]">
                {displayRepo(execution.repo, execution.repo_url)}
              </td>
              <td className="px-4 py-2 font-mono">{execution.ref || execution.default_branch || "--"}</td>
              <td className="px-4 py-2">
                <StatusBadge status={execution.status} />
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

function ExecutionTableSkeleton() {
  return (
    <div className="border border-border rounded-lg p-8 text-center text-muted-foreground">
      Sign in to view your sandboxes.
    </div>
  );
}

function displayRepo(repo: string, repoURL: string): string {
  if (repo) return repo;
  if (!repoURL) return "--";
  return repoURL.replace("https://", "");
}
