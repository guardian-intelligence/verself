import type { EntitlementSlot, EntitlementsView } from "~/server-fns/api";
import { formatLedgerAmount } from "~/lib/format";

// Display-only presentation of the honest slot model. The server returns
// the four-scope slot tree (account → product → bucket → sku); this panel
// renders only the account-scoped "Account Balance" header. Per-SKU coverage
// lookups now live in features/billing/state (CreditsProductState.bySKU),
// which the Usage table consumes directly instead of re-walking the tree.

export function EntitlementsPanel({ view }: { view: EntitlementsView }) {
  // The hidden `data-test-available-units` attribute is a deliberate
  // cross-slot sum used by the e2e harness's `readBalance()` for *relative*
  // comparisons (started_balance vs finished_balance). It double-counts in
  // every honest sense — different slots cover different scopes the funder
  // drains in a fixed order — and is never displayed to users. Do not render
  // this value, and do not "fix" it into a top-line total.
  const totalAvailableForTests = collectVisibleSlots(view).reduce(
    (acc, slot) => acc + slot.available_units,
    0,
  );

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
    <div
      className="space-y-6"
      data-testid="entitlements-view"
      data-test-available-units={totalAvailableForTests}
    >
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
      <div className="text-xs uppercase tracking-wide text-muted-foreground">Account Balance</div>
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
      <p className="text-sm text-muted-foreground max-w-xl">
        Account balance is only deducted after all other credit sources for a product have been
        used.
      </p>
    </section>
  );
}

function collectVisibleSlots(view: EntitlementsView): EntitlementSlot[] {
  const slots: EntitlementSlot[] = [view.universal];
  for (const product of view.products ?? []) {
    if (product.product_slot) slots.push(product.product_slot);
    for (const bucket of product.buckets ?? []) {
      if (bucket.bucket_slot) slots.push(bucket.bucket_slot);
      for (const sku of bucket.sku_slots ?? []) slots.push(sku);
    }
  }
  return slots;
}
