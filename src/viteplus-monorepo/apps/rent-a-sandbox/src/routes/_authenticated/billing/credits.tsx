import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { BalanceCard } from "~/components/balance-card";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateCheckoutSessionMutation } from "~/features/billing/mutations";
import { balanceQuery, loadBalance } from "~/features/billing/queries";

export const Route = createFileRoute("/_authenticated/billing/credits")({
  loader: ({ context }) => loadBalance(context.queryClient, context.auth),
  component: CreditsPage,
});

const CREDIT_PACKS = [
  { cents: 1000, label: "$10" },
  { cents: 2500, label: "$25" },
  { cents: 5000, label: "$50" },
  { cents: 10000, label: "$100" },
];

function CreditsPage() {
  const auth = useSignedInAuth();
  const balance = useSuspenseQuery(balanceQuery(auth)).data;
  const mutation = useCreateCheckoutSessionMutation();

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Purchase Credits</h1>
      <p className="text-muted-foreground">
        Add prepaid account balance for usage beyond your current bucket allowances.
      </p>

      <div className="max-w-md">
        <BalanceCard balance={balance} />
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        {CREDIT_PACKS.map((pack) => (
          <button
            key={pack.cents}
            type="button"
            onClick={() => mutation.mutate(pack.cents)}
            disabled={mutation.isPending}
            className="rounded-lg border border-border p-4 text-center transition-colors hover:bg-accent disabled:opacity-50"
          >
            <div className="text-2xl font-bold">{pack.label}</div>
            <div className="text-sm text-muted-foreground mt-1">Account top-up</div>
          </button>
        ))}
      </div>

      {mutation.error ? <ErrorCallout error={mutation.error} title="Checkout failed" /> : null}
    </div>
  );
}
