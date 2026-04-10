import { createFileRoute, Link } from "@tanstack/react-router";
import { Callout } from "~/components/callout";
import { ExecutionListPanel } from "~/features/jobs/components";
import { loadJobsIndex } from "~/features/jobs/queries";

export const Route = createFileRoute("/_authenticated/jobs/")({
  loader: ({ context }) => loadJobsIndex(context.queryClient, context.auth),
  component: JobsPage,
});

function JobsPage() {
  const balance = Route.useLoaderData();
  const { auth } = Route.useRouteContext();

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

      {auth.orgId ? (
        <ExecutionListPanel orgId={auth.orgId} />
      ) : (
        <Callout tone="destructive" title="Missing organization">
          Your session does not include a Zitadel resource owner ID, so executions cannot be
          scoped safely.
        </Callout>
      )}
    </div>
  );
}
