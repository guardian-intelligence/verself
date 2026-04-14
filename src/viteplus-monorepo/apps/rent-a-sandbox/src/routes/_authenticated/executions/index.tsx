import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Callout } from "~/components/callout";
import { useBillingAccount } from "~/features/billing/use-billing-account";
import { ExecutionListPanel } from "~/features/executions/components";
import { loadExecutionsIndex } from "~/features/executions/queries";

export const Route = createFileRoute("/_authenticated/executions/")({
  loader: ({ context }) => loadExecutionsIndex(context.queryClient, context.auth),
  component: ExecutionsPage,
});

function ExecutionsPage() {
  const { auth } = Route.useRouteContext();
  // The loader already derived and warmed the snapshot; this hook re-runs
  // the pure selector on the cached query data so client refetches stay
  // honest.
  const { account } = useBillingAccount();
  const creditsExhausted = account.credits.kind === "exhausted";

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Executions</h1>
          <p className="text-sm text-muted-foreground">
            Direct VM executions, billing windows, and logs.
          </p>
        </div>
        {creditsExhausted ? (
          <Button
            type="button"
            variant="outline"
            disabled
            data-testid="new-execution-disabled"
            title="No credits remaining — purchase more or subscribe to a plan"
          >
            New execution
          </Button>
        ) : (
          <Button asChild variant="default">
            <Link to="/executions/new" data-testid="new-execution">
              New execution
            </Link>
          </Button>
        )}
      </header>

      {creditsExhausted ? <CreditsExhaustedCallout accountKind={account.kind} /> : null}

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

function CreditsExhaustedCallout({ accountKind }: { accountKind: string }) {
  // Subscribers whose plan allowance is drained get pointed at the top-up
  // page; users with no contract at all get pointed at the plan picker.
  const ctaTarget =
    accountKind === "no_contract" ? "/settings/billing/subscribe" : "/settings/billing/credits";
  const ctaLabel = accountKind === "no_contract" ? "Choose plan" : "Buy credits";

  return (
    <Callout
      tone="warning"
      title="Your credit balance is empty"
      action={
        <Button asChild variant="default">
          <Link to={ctaTarget} data-testid="credits-exhausted-cta">
            {ctaLabel}
          </Link>
        </Button>
      }
    >
      Purchase credits or pick a plan to start executions.
    </Callout>
  );
}
