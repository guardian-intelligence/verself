import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateSubscriptionSessionMutation } from "~/features/billing/mutations";
import { plansQuery } from "~/features/billing/queries";
import { formatCents } from "~/lib/format";

export const Route = createFileRoute("/_authenticated/billing/subscribe")({
  loader: ({ context }) => context.queryClient.ensureQueryData(plansQuery(context.auth)),
  component: SubscribePage,
});

function SubscribePage() {
  const auth = useSignedInAuth();
  const mutation = useCreateSubscriptionSessionMutation();
  const plans = useSuspenseQuery(plansQuery(auth)).data.plans ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Subscribe to get monthly bucketed allowances for sandbox usage.
      </p>

      {plans.length > 0 ? (
        <div className="grid md:grid-cols-3 gap-4">
          {plans.map((plan) => (
            <div
              key={plan.plan_id}
              data-testid={`subscription-plan-${plan.plan_id}`}
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
                data-testid={`subscribe-plan-${plan.plan_id}`}
                onClick={() => mutation.mutate(plan.plan_id)}
                disabled={mutation.isPending}
                className="mt-auto px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
              >
                {mutation.isPending ? "Redirecting..." : `Subscribe to ${plan.display_name}`}
              </button>
            </div>
          ))}
        </div>
      ) : (
        <div className="border border-border rounded-lg p-6 text-sm text-muted-foreground">
          No subscription plans are currently available.
        </div>
      )}

      {mutation.error ? (
        <ErrorCallout error={mutation.error} title="Subscription checkout failed" />
      ) : null}
    </div>
  );
}
