import { createFileRoute, Link } from "@tanstack/react-router";
import { Button } from "@forge-metal/ui/components/ui/button";
import { ErrorCallout } from "~/components/error-callout";
import { useCreateCheckoutSessionMutation } from "~/features/billing/mutations";

export const Route = createFileRoute("/_authenticated/settings/billing/credits")({
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
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h2 className="font-mono text-xs uppercase tracking-[0.2em] text-muted-foreground">
            Credits
          </h2>
          <p className="text-2xl font-semibold">Purchase credits</p>
          <p className="text-sm text-muted-foreground">
            Add prepaid account balance for usage beyond your current bucket allowances.
          </p>
        </div>
        <Link
          to="/settings/billing"
          className="font-mono text-xs uppercase tracking-wider text-muted-foreground hover:text-foreground"
        >
          ← Back to billing
        </Link>
      </div>

      <div className="grid gap-4 md:grid-cols-4">
        {CREDIT_PACKS.map((pack) => (
          <Button
            key={pack.cents}
            type="button"
            variant="outline"
            onClick={() => mutation.mutate(pack.cents)}
            disabled={mutation.isPending}
            className="flex h-auto flex-col items-center gap-1 rounded-none border-foreground py-6"
          >
            <span className="font-mono text-2xl font-semibold tabular-nums">{pack.label}</span>
            <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
              Account top-up
            </span>
          </Button>
        ))}
      </div>

      {mutation.error ? <ErrorCallout error={mutation.error} title="Checkout failed" /> : null}
    </div>
  );
}
