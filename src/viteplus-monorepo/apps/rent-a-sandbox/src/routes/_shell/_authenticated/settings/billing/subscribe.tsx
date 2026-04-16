import { useMemo } from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@forge-metal/ui/components/ui/card";
import {
  PageEyebrow,
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { ErrorCallout } from "~/components/error-callout";
import { usePlanCardActionMutation } from "~/features/billing/mutations";
import { loadSubscribePage } from "~/features/billing/queries";
import {
  assertUnreachable,
  deriveAllPlanCards,
  intentFor,
  type BillingAccount,
  type PlanCardIntent,
  type PlanCardState,
} from "~/features/billing/state";
import { useBillingAccount } from "~/features/billing/use-billing-account";
import { formatCents, formatDateTimeUTC } from "~/lib/format";

export const Route = createFileRoute("/_shell/_authenticated/settings/billing/subscribe")({
  loader: ({ context }) => loadSubscribePage(context.queryClient, context.auth),
  component: SubscribePage,
});

function SubscribePage() {
  const initial = Route.useLoaderData();
  const { account, snapshot } = useBillingAccount({
    initialPlans: initial.plans,
    initialContracts: initial.contracts,
    initialEntitlements: initial.entitlements,
  });
  const mutation = usePlanCardActionMutation();
  const plans = snapshot.plans.plans ?? [];
  const cards = useMemo(() => deriveAllPlanCards(account, plans), [account, plans]);

  return (
    <PageSections>
      <PageSection>
        <PageEyebrow>
          <Link to="/settings/billing" className="hover:text-foreground">
            ← Back to billing
          </Link>
        </PageEyebrow>
        <SectionHeader>
          <SectionHeaderContent>
            <SectionTitle>Choose a plan</SectionTitle>
            <SectionDescription>
              Create a contract to get monthly bucketed allowances for sandbox usage.
            </SectionDescription>
          </SectionHeaderContent>
        </SectionHeader>

        {plans.length > 0 ? (
          <div className="grid gap-4 md:grid-cols-3">
            {cards.map((card) => {
              const intent = intentFor(card, account);
              return (
                <PlanCardView
                  key={card.plan.plan_id}
                  account={account}
                  card={card}
                  intent={intent}
                  isPending={mutation.isPending}
                  onClick={() => mutation.mutate({ intent, plan: card.plan })}
                />
              );
            })}
          </div>
        ) : (
          <div className="rounded-md border bg-card p-6 text-sm text-muted-foreground">
            No contract plans are currently available.
          </div>
        )}

        {mutation.error ? (
          <ErrorCallout error={mutation.error} title="Contract checkout failed" />
        ) : null}
      </PageSection>
    </PageSections>
  );
}

interface PlanCardViewProps {
  readonly account: BillingAccount;
  readonly card: PlanCardState;
  readonly intent: PlanCardIntent;
  readonly isPending: boolean;
  readonly onClick: () => void;
}

function PlanCardView({ account, card, intent, isPending, onClick }: PlanCardViewProps) {
  const { plan } = card;
  const copy = planCardCopy(card, account, isPending);
  const disabled = isPending || intent.kind === "disabled";
  const current = card.kind === "current" || card.kind === "current_resumable";

  return (
    <Card
      data-testid={`contract-plan-${plan.plan_id}`}
      data-card-kind={card.kind}
      className="flex h-full flex-col"
    >
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle>{plan.display_name}</CardTitle>
          {current ? <Badge variant="secondary">Current</Badge> : null}
        </div>
        <p className="text-xs text-muted-foreground">
          Monthly sandbox usage allowance for the {plan.tier} tier.
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="font-mono text-3xl font-semibold tabular-nums">
          {formatCents(plan.monthly_amount_cents, plan.currency)}
          <span className="ml-1 text-sm font-normal text-muted-foreground">/mo</span>
        </div>
        {copy.hint ? <p className="text-xs text-muted-foreground">{copy.hint}</p> : null}
      </CardContent>
      <CardFooter className="mt-auto">
        <Button
          type="button"
          variant={current ? "outline" : "default"}
          data-testid={`start-contract-plan-${plan.plan_id}`}
          onClick={onClick}
          disabled={disabled}
          title={copy.tooltip}
          className="w-full"
        >
          {copy.label}
        </Button>
      </CardFooter>
    </Card>
  );
}

interface PlanCardCopy {
  readonly label: string;
  readonly hint?: string;
  readonly tooltip?: string;
}

// Single place where PlanCardState maps to display copy. Every new kind added
// to the union forces a new branch here thanks to assertUnreachable below, so
// you cannot ship a card state that renders blank.
function planCardCopy(
  card: PlanCardState,
  account: BillingAccount,
  isPending: boolean,
): PlanCardCopy {
  if (isPending) {
    return { label: "Redirecting…" };
  }

  switch (card.kind) {
    case "fresh_start":
      return { label: `Start ${card.plan.display_name}` };
    case "current":
      return {
        label: "Current plan",
        tooltip: "You are already subscribed to this plan.",
      };
    case "current_resumable":
      return {
        label: `Resume ${card.plan.display_name}`,
        hint: "A plan change is scheduled — click to resume this plan.",
      };
    case "upgrade_target":
      return {
        label: `Upgrade to ${card.plan.display_name}`,
        hint: "Takes effect immediately with a prorated charge.",
      };
    case "downgrade_target":
      return {
        label: `Schedule ${card.plan.display_name} downgrade`,
        hint: "Takes effect at the end of the current billing cycle.",
      };
    case "scheduled_downgrade":
      return {
        label: `${card.plan.display_name} downgrade scheduled`,
        hint: formatScheduledDowngradeHint(card.effectiveAt),
        tooltip: "This downgrade is already scheduled. Click your current plan to resume it.",
      };
    case "locked_by_pending_change":
      return {
        label: "Plan change unavailable",
        hint: lockedByPendingChangeHint(account),
        tooltip:
          "A plan change is already scheduled. Resume or wait for it to take effect before switching plans.",
      };
    default:
      return assertUnreachable(card);
  }
}

function formatScheduledDowngradeHint(effectiveAt: Date | null): string {
  if (!effectiveAt) {
    return "Applies at the next billing cycle.";
  }
  return `Applies at ${formatDateTimeUTC(effectiveAt)}.`;
}

function lockedByPendingChangeHint(account: BillingAccount): string {
  switch (account.kind) {
    case "pending_downgrade":
      return `Downgrade to ${account.target.display_name} is already scheduled.`;
    case "pending_cancel":
      return "Cancellation is already scheduled.";
    case "no_contract":
    case "active":
      return "A plan change is already in flight.";
    default:
      return assertUnreachable(account);
  }
}
