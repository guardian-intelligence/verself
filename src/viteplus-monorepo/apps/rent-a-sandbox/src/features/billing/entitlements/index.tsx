import type { EntitlementsView } from "~/server-fns/api";
import { formatLedgerAmount } from "~/lib/format";

// Display-only presentation of the honest slot model. The server returns
// the four-scope slot tree (account → product → bucket → sku); this panel
// renders only the account-scoped "Account Balance" header. Per-SKU coverage
// lookups live in features/billing/state (CreditsProductState.bySKU),
// which the Usage table consumes directly instead of re-walking the tree.

export function EntitlementsPanel({ view }: { view: EntitlementsView }) {
  const accountBalance = (view.universal?.sources ?? [])
    .filter((source) => source.source === "purchase")
    .reduce(
      (acc, source) => ({
        availableUnits: acc.availableUnits + source.available_units,
        pendingUnits: acc.pendingUnits + source.pending_units,
      }),
      { availableUnits: 0, pendingUnits: 0 },
    );

  return (
    <div className="space-y-6" data-testid="entitlements-view">
      <AccountBalanceHeader
        availableUnits={accountBalance.availableUnits}
        pendingUnits={accountBalance.pendingUnits}
      />
    </div>
  );
}

function AccountBalanceHeader({
  availableUnits,
  pendingUnits,
}: {
  availableUnits: number;
  pendingUnits: number;
}) {
  const totalUnits = availableUnits + pendingUnits;
  return (
    <section
      className="space-y-1"
      data-testid="entitlements-account-balance"
      data-account-balance-units={totalUnits}
      data-account-balance-pending-units={pendingUnits}
    >
      <div className="text-sm text-muted-foreground">Account balance</div>
      <div
        className="font-mono text-3xl font-semibold tabular-nums"
        data-testid="account-balance-value"
      >
        {formatLedgerAmount(totalUnits)}
      </div>
      {pendingUnits > 0 ? (
        <p className="text-sm text-muted-foreground">
          {formatLedgerAmount(pendingUnits)} is reserved for running executions.
        </p>
      ) : null}
      <p className="max-w-xl text-sm text-muted-foreground">
        Account balance is only deducted after all other credit sources for a product have been
        used.
      </p>
    </section>
  );
}
