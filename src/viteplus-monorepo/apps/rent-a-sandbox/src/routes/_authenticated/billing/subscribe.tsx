import { createFileRoute } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { usePlanCardActionMutation } from "~/features/billing/mutations";
import { loadSubscribePage } from "~/features/billing/queries";
import {
  assertUnreachable,
  derivePlanCard,
  intentFor,
  type BillingAccount,
  type PlanCardIntent,
  type PlanCardState,
} from "~/features/billing/state";
import { useBillingAccount } from "~/features/billing/use-billing-account";
import { formatCents, formatDateTimeUTC } from "~/lib/format";

export const Route = createFileRoute("/_authenticated/billing/subscribe")({
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

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Create a contract to get monthly bucketed allowances for sandbox usage.
      </p>

      {plans.length > 0 ? (
        <div className="grid md:grid-cols-3 gap-4">
          {plans.map((plan) => {
            const card = derivePlanCard(account, plan);
            const intent = intentFor(card, account);
            return (
              <PlanCardView
                key={plan.plan_id}
                account={account}
                card={card}
                intent={intent}
                isPending={mutation.isPending}
                onClick={() => mutation.mutate({ intent, plan })}
              />
            );
          })}
        </div>
      ) : (
        <div className="border border-border rounded-lg p-6 text-sm text-muted-foreground">
          No contract plans are currently available.
        </div>
      )}

      {mutation.error ? (
        <ErrorCallout error={mutation.error} title="Contract checkout failed" />
      ) : null}
    </div>
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

  return (
    <div
      data-testid={`contract-plan-${plan.plan_id}`}
      data-card-kind={card.kind}
      className="border border-border rounded-lg p-6 flex flex-col gap-4"
    >
      <div>
        <h3 className="text-lg font-semibold">{plan.display_name}</h3>
        <p className="text-muted-foreground text-sm">
          Monthly sandbox usage allowance for the {plan.tier} tier.
        </p>
      </div>
      <div className="text-2xl font-bold">
        {formatCents(plan.monthly_amount_cents, plan.currency)}/mo
      </div>
      {copy.hint ? <p className="text-xs text-muted-foreground">{copy.hint}</p> : null}
      <button
        type="button"
        data-testid={`start-contract-plan-${plan.plan_id}`}
        onClick={onClick}
        disabled={disabled}
        title={copy.tooltip}
        className="mt-auto px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
      >
        {copy.label}
      </button>
    </div>
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
    return { label: "Redirecting..." };
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
        hint: `Takes effect immediately with a prorated charge.`,
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
