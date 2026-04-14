import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { ErrorCallout } from "~/components/error-callout";
import {
  useCreateContractChangeSessionMutation,
  useCreateContractSessionMutation,
} from "~/features/billing/mutations";
import { contractsQuery, plansQuery } from "~/features/billing/queries";
import { formatCents } from "~/lib/format";

export const Route = createFileRoute("/_authenticated/billing/subscribe")({
  loader: async ({ context }) => {
    const [plans, contracts] = await Promise.all([
      context.queryClient.ensureQueryData(plansQuery(context.auth)),
      context.queryClient.ensureQueryData(contractsQuery(context.auth)),
    ]);
    return { plans, contracts };
  },
  component: SubscribePage,
});

function SubscribePage() {
  const auth = useSignedInAuth();
  const createMutation = useCreateContractSessionMutation();
  const changeMutation = useCreateContractChangeSessionMutation();
  const initial = Route.useLoaderData();
  const plans =
    useSuspenseQuery({
      ...plansQuery(auth),
      initialData: initial.plans,
    }).data.plans ?? [];
  const contracts =
    useSuspenseQuery({
      ...contractsQuery(auth),
      initialData: initial.contracts,
    }).data.contracts ?? [];
  const activeContract = contracts.find(
    (contract) =>
      contract.product_id === "sandbox" &&
      (contract.status === "active" || contract.status === "cancel_scheduled") &&
      contract.plan_id,
  );
  const activePlan = plans.find((plan) => plan.plan_id === activeContract?.plan_id);
  const isPending = createMutation.isPending || changeMutation.isPending;
  const mutationError = createMutation.error ?? changeMutation.error;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Create a contract to get monthly bucketed allowances for sandbox usage.
      </p>

      {plans.length > 0 ? (
        <div className="grid md:grid-cols-3 gap-4">
          {plans.map((plan) => {
            const isCurrentPlan = activeContract?.plan_id === plan.plan_id;
            const pendingChangeType = activeContract?.pending_change_type ?? "";
            const hasPendingPlanChange =
              pendingChangeType === "downgrade" || pendingChangeType === "cancel";
            const canResumeCurrentPlan = isCurrentPlan && hasPendingPlanChange;
            const isScheduledDowngradeTarget =
              pendingChangeType === "downgrade" &&
              activeContract?.pending_change_target_plan_id === plan.plan_id;
            const isSupportedUpgrade =
              activeContract &&
              activePlan &&
              !hasPendingPlanChange &&
              plan.monthly_amount_cents > activePlan.monthly_amount_cents;
            const isSupportedPlanChange = Boolean(
              activeContract && activePlan && !isCurrentPlan && !hasPendingPlanChange,
            );
            return (
              <div
                key={plan.plan_id}
                data-testid={`contract-plan-${plan.plan_id}`}
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
                <button
                  type="button"
                  data-testid={`start-contract-plan-${plan.plan_id}`}
                  onClick={() => {
                    if ((isSupportedPlanChange || canResumeCurrentPlan) && activeContract) {
                      changeMutation.mutate({
                        contractId: activeContract.contract_id,
                        targetPlanId: plan.plan_id,
                        action: canResumeCurrentPlan
                          ? "resume"
                          : isSupportedUpgrade
                            ? "upgrade"
                            : "downgrade",
                      });
                      return;
                    }
                    createMutation.mutate({ planId: plan.plan_id, action: "start" });
                  }}
                  disabled={
                    isPending ||
                    Boolean(activeContract && !isSupportedPlanChange && !canResumeCurrentPlan)
                  }
                  className="mt-auto px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
                >
                  {isPending
                    ? "Redirecting..."
                    : canResumeCurrentPlan
                      ? `Resume ${plan.display_name}`
                      : isCurrentPlan
                        ? "Current plan"
                        : isScheduledDowngradeTarget
                          ? `${plan.display_name} downgrade scheduled`
                          : hasPendingPlanChange
                            ? "Plan change pending"
                            : isSupportedUpgrade
                              ? `Upgrade to ${plan.display_name}`
                              : isSupportedPlanChange
                                ? `Schedule ${plan.display_name} downgrade`
                                : activeContract
                                  ? "Plan change unavailable"
                                  : `Start ${plan.display_name}`}
                </button>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="border border-border rounded-lg p-6 text-sm text-muted-foreground">
          No contract plans are currently available.
        </div>
      )}

      {mutationError ? (
        <ErrorCallout error={mutationError} title="Contract checkout failed" />
      ) : null}
    </div>
  );
}
