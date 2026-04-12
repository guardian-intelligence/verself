import { createFileRoute } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateSubscriptionSessionMutation } from "~/features/billing/mutations";

export const Route = createFileRoute("/_authenticated/billing/subscribe")({
  component: SubscribePage,
});

const PLANS = [
  {
    id: "sandbox-starter",
    name: "Starter",
    description: "Monthly sandbox usage allowance",
    price: "$29/mo",
  },
  {
    id: "sandbox-pro",
    name: "Pro",
    description: "Larger monthly sandbox allowance",
    price: "$99/mo",
  },
  {
    id: "sandbox-team",
    name: "Team",
    description: "Team sandbox usage allowance",
    price: "$299/mo",
  },
];

function SubscribePage() {
  const mutation = useCreateSubscriptionSessionMutation();

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Subscribe to get monthly bucketed allowances for sandbox usage.
      </p>

      <div className="grid md:grid-cols-3 gap-4">
        {PLANS.map((plan) => (
          <div key={plan.id} className="border border-border rounded-lg p-6 flex flex-col gap-4">
            <div>
              <h3 className="text-lg font-semibold">{plan.name}</h3>
              <p className="text-muted-foreground text-sm">{plan.description}</p>
            </div>
            <div className="text-2xl font-bold">{plan.price}</div>
            <button
              type="button"
              onClick={() => mutation.mutate(plan.id)}
              disabled={mutation.isPending}
              className="mt-auto px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
            >
              {mutation.isPending ? "Redirecting..." : "Subscribe"}
            </button>
          </div>
        ))}
      </div>

      {mutation.error ? (
        <ErrorCallout error={mutation.error} title="Subscription checkout failed" />
      ) : null}
    </div>
  );
}
