import { createFileRoute } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateSubscriptionSessionMutation } from "~/features/billing/mutations";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/billing/subscribe")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  component: SubscribePage,
});

const PLANS = [
  {
    id: "sandbox-starter",
    name: "Starter",
    description: "10,000 credits/month",
    price: "$29/mo",
  },
  {
    id: "sandbox-pro",
    name: "Pro",
    description: "50,000 credits/month",
    price: "$99/mo",
  },
  {
    id: "sandbox-team",
    name: "Team",
    description: "200,000 credits/month",
    price: "$299/mo",
  },
];

function SubscribePage() {
  const mutation = useCreateSubscriptionSessionMutation();

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Subscribe to get monthly credit allowances for sandbox usage.
      </p>

      <div className="grid gap-4 md:grid-cols-3">
        {PLANS.map((plan) => (
          <div key={plan.id} className="flex flex-col gap-4 rounded-lg border border-border p-6">
            <div>
              <h3 className="text-lg font-semibold">{plan.name}</h3>
              <p className="text-sm text-muted-foreground">{plan.description}</p>
            </div>
            <div className="text-2xl font-bold">{plan.price}</div>
            <button
              type="button"
              onClick={() => mutation.mutate(plan.id)}
              disabled={mutation.isPending}
              className="mt-auto rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90 disabled:opacity-50"
            >
              {mutation.isPending ? "Redirecting..." : "Subscribe"}
            </button>
          </div>
        ))}
      </div>

      {mutation.error ? <ErrorCallout error={mutation.error} title="Subscription checkout failed" /> : null}
    </div>
  );
}
