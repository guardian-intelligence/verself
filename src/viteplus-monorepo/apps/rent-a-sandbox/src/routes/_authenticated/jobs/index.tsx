import { createFileRoute, Link } from "@tanstack/react-router";
import { Callout } from "~/components/callout";
import { useBillingAccount } from "~/features/billing/use-billing-account";
import { ExecutionListPanel } from "~/features/jobs/components";
import { loadJobsIndex } from "~/features/jobs/queries";

export const Route = createFileRoute("/_authenticated/jobs/")({
  loader: ({ context }) => loadJobsIndex(context.queryClient, context.auth),
  component: JobsPage,
});

function JobsPage() {
  const { auth } = Route.useRouteContext();
  // The loader already derived and warmed the snapshot; this hook re-runs the
  // pure selector on the cached query data so client refetches stay honest.
  const { account } = useBillingAccount();
  const creditsExhausted = account.credits.kind === "exhausted";

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-bold">Executions</h1>
          <p className="text-sm text-muted-foreground">
            Direct VM executions, billing windows, and logs.
          </p>
        </div>
        {creditsExhausted ? (
          <span
            title="No credits remaining — purchase more or subscribe to a plan"
            data-testid="new-execution-disabled"
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

      {creditsExhausted ? <CreditsExhaustedCallout accountKind={account.kind} /> : null}

      {auth.orgId ? (
        <ExecutionListPanel orgId={auth.orgId} />
      ) : (
        <Callout tone="destructive" title="Missing organization">
          Your session does not include a Zitadel resource owner ID, so executions cannot be scoped
          safely.
        </Callout>
      )}
    </div>
  );
}

function CreditsExhaustedCallout({ accountKind }: { accountKind: string }) {
  // Subscribers whose plan allowance is drained get pointed at the top-up
  // page; users with no contract at all get pointed at the plan picker.
  const ctaTarget = accountKind === "no_contract" ? "/billing/subscribe" : "/billing/credits";
  const ctaLabel = accountKind === "no_contract" ? "Choose Plan" : "Buy Credits";

  return (
    <Callout
      tone="warning"
      title="Your credit balance is empty"
      action={
        <Link
          to={ctaTarget}
          data-testid="credits-exhausted-cta"
          className="whitespace-nowrap rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:opacity-90"
        >
          {ctaLabel}
        </Link>
      }
    >
      Purchase credits or pick a plan to start executions.
    </Callout>
  );
}
