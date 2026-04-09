import { createFileRoute, Link } from "@tanstack/react-router";
import { requireViewer } from "~/lib/protected-route";
import { jobsBalanceQuery } from "~/features/jobs/queries";
import { ExecutionListPanel } from "~/features/jobs/components";
import { Callout } from "~/components/callout";

export const Route = createFileRoute("/jobs/")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(jobsBalanceQuery()),
  component: JobsPage,
});

function JobsPage() {
  const balance = Route.useLoaderData();

  const creditsExhausted = balance.total_available <= 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-bold">Executions</h1>
          <p className="text-sm text-muted-foreground">
            Repo runs, golden preparation attempts, and VM-backed runner jobs.
          </p>
        </div>
        {creditsExhausted ? (
          <span
            title="No credits remaining - purchase more at /billing/credits"
            className="px-4 py-2 rounded-md bg-muted text-muted-foreground text-sm cursor-not-allowed"
          >
            New Execution
          </span>
        ) : (
          <Link
            to="/jobs/new"
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
          >
            Manual Execution
          </Link>
        )}
      </div>

      {creditsExhausted && (
        <Callout
          tone="warning"
          title="Your credit balance is empty"
          action={
            <Link
              to="/billing/credits"
              className="whitespace-nowrap rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:opacity-90"
            >
              Buy Credits
            </Link>
          }
        >
          Purchase credits to start executions.
        </Callout>
      )}

      <ExecutionListPanel orgId={balance.org_id} />
    </div>
  );
}
