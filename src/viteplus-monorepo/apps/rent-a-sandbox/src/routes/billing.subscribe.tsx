import { createFileRoute } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { createSubscriptionSession } from "~/server-fns/api";
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
  const mutation = useMutation({
    mutationFn: (planId: string) =>
      createSubscriptionSession({
        data: {
        plan_id: planId,
        cadence: "monthly",
        success_url: `${window.location.origin}/billing?subscribed=true`,
        cancel_url: `${window.location.origin}/billing/subscribe`,
        },
      }),
    onSuccess: (data) => {
      window.location.href = data.url;
    },
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Choose a Plan</h1>
      <p className="text-muted-foreground">
        Subscribe to get monthly credit allowances for sandbox usage.
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
              onClick={() => mutation.mutate(plan.id)}
              disabled={mutation.isPending}
              className="mt-auto px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm disabled:opacity-50"
            >
              {mutation.isPending ? "Redirecting..." : "Subscribe"}
            </button>
          </div>
        ))}
      </div>

      {mutation.error && <p className="text-sm text-destructive">{mutation.error.message}</p>}
    </div>
  );
}
