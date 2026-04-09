import { createFileRoute } from "@tanstack/react-router";
import { BalanceCard } from "~/components/balance-card";
import { useCreateCheckoutSessionMutation } from "~/features/billing/mutations";
import { loadBalance } from "~/features/billing/queries";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/billing/credits")({
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context }) => loadBalance(context.queryClient),
  component: CreditsPage,
});

const CREDIT_PACKS = [
  { cents: 1000, label: "$10", credits: "~3,300 credits" },
  { cents: 2500, label: "$25", credits: "~8,300 credits" },
  { cents: 5000, label: "$50", credits: "~16,600 credits" },
  { cents: 10000, label: "$100", credits: "~33,300 credits" },
];

function CreditsPage() {
  const balance = Route.useLoaderData();
  const mutation = useCreateCheckoutSessionMutation({
    onSuccess: (data) => {
      window.location.href = data.url;
    },
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Purchase Credits</h1>
      <p className="text-muted-foreground">
        Buy additional credits to top up your balance. Credits are added instantly.
      </p>

      <div className="max-w-md">
        <BalanceCard balance={balance} />
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {CREDIT_PACKS.map((pack) => (
          <button
            key={pack.cents}
            onClick={() => mutation.mutate(pack.cents)}
            disabled={mutation.isPending}
            className="border border-border rounded-lg p-4 text-center hover:bg-accent disabled:opacity-50 transition-colors"
          >
            <div className="text-2xl font-bold">{pack.label}</div>
            <div className="text-sm text-muted-foreground mt-1">{pack.credits}</div>
          </button>
        ))}
      </div>

      {mutation.error ? <p className="text-sm text-destructive">{mutation.error.message}</p> : null}
    </div>
  );
}
