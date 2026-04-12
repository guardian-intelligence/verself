import { createFileRoute, Link } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateCheckoutSessionMutation } from "~/features/billing/mutations";

export const Route = createFileRoute("/_authenticated/billing/credits")({
  component: CreditsPage,
});

const CREDIT_PACKS = [
  { cents: 1000, label: "$10" },
  { cents: 2500, label: "$25" },
  { cents: 5000, label: "$50" },
  { cents: 10000, label: "$100" },
];

function CreditsPage() {
  const mutation = useCreateCheckoutSessionMutation();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Purchase Credits</h1>
        <Link
          to="/billing"
          className="text-sm text-muted-foreground underline hover:text-foreground"
        >
          Back to billing
        </Link>
      </div>
      <p className="text-muted-foreground">
        Add prepaid account balance for usage beyond your current bucket allowances.
      </p>

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
