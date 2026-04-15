import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  Page,
  PageActions,
  PageDescription,
  PageHeader,
  PageHeaderContent,
  PageSection,
  PageSections,
  PageTitle,
} from "@forge-metal/ui/components/ui/page";
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
    <Page>
      <PageHeader>
        <PageHeaderContent>
          <PageTitle>Executions</PageTitle>
          <PageDescription>Direct VM executions, billing windows, and logs.</PageDescription>
        </PageHeaderContent>
        <PageActions>
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
            <Button
              variant="default"
              render={<Link to="/executions/new" data-testid="new-execution" />}
            >
              New execution
            </Button>
          )}
        </PageActions>
      </PageHeader>

      <PageSections>
        {creditsExhausted ? (
          <PageSection>
            <CreditsExhaustedCallout accountKind={account.kind} />
          </PageSection>
        ) : null}

        <PageSection>
          {auth.orgId ? (
            <ExecutionListPanel orgId={auth.orgId} />
          ) : (
            <Callout tone="destructive" title="Missing organization">
              Your session is missing organization context. Try signing out and back in.
            </Callout>
          )}
        </PageSection>
      </PageSections>
    </Page>
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
        <Button
          variant="default"
          render={<Link to={ctaTarget} data-testid="credits-exhausted-cta" />}
        >
          {ctaLabel}
        </Button>
      }
    >
      Purchase credits or pick a plan to start executions.
    </Callout>
  );
}
