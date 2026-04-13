import type { EntitlementSlot, EntitlementSourceTotal, EntitlementsView } from "~/server-fns/api";
import { formatLedgerAmount } from "~/lib/format";

// Display-only presentation of the honest slot model. The server returns
// the four-scope slot tree (account → product → bucket → sku); this panel
// renders only the account-scoped "Account Balance" header and exposes a
// per-SKU source lookup that the Usage table uses to annotate each drain row
// with its remaining balance. The dedicated Credit Balances table was
// intentionally removed — per-SKU coverage is now surfaced inline next to the
// drain amount it will cover, not duplicated in a parallel table.
//
// Pooling note: bucket- and product-scoped sources (e.g. a Hobby plan's
// per-bucket allotment) appear in every SKU's combined source list. That is
// deliberately optimistic — each row answers "how much juice could this SKU
// draw, ignoring contention with siblings" — and matches how the funder
// actually drains: most-specific scope first, then pooled scopes. Do not try
// to "fix" the apparent over-count across SKUs; it's the whole reason the slot
// tree exists. Purchase (account balance) is the only source that is NOT
// pooled into per-SKU lookups, because the Account Balance header tells the
// customer it only drains after everything else.

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

// SKURemainingLookup is keyed by SKU id. Each entry is the combined
// (sku ∪ bucket ∪ product) source list for that SKU, in the same order the
// funder drains: most-specific source first, pooled scopes last. The Usage
// table uses this to render "Free tier ($0.27 remaining)" next to each drain
// row, with the source's Label and AvailableUnits straight from this list.
//
// Purchase (account-scoped) sources are intentionally excluded — the Account
// Balance header above already surfaces them, and the Usage drain row for
// "Account balance" doesn't show a remaining suffix.
export type SKURemainingLookup = Map<string, EntitlementSourceTotal[]>;

export function buildSKURemainingLookup(view: EntitlementsView): SKURemainingLookup {
  const map: SKURemainingLookup = new Map();
  for (const section of view.products ?? []) {
    const productSources = section.product_slot?.sources ?? [];
    for (const bucket of section.buckets ?? []) {
      const bucketSources = bucket.bucket_slot?.sources ?? [];
      for (const sku of bucket.sku_slots ?? []) {
        map.set(sku.sku_id, combineSources([...sku.sources, ...bucketSources, ...productSources]));
      }
    }
  }
  return map;
}

function combineSources(sources: EntitlementSourceTotal[]): EntitlementSourceTotal[] {
  const rank: Record<string, number> = {
    free_tier: 0,
    contract: 1,
    promo: 2,
    refund: 3,
    purchase: 4,
  };
  const byKey = new Map<string, EntitlementSourceTotal>();
  for (const source of sources) {
    if (source.available_units === 0) continue;
    // Account-scoped top-ups surface only via the Account Balance header.
    if (source.source === "purchase") continue;
    const key = `${source.source}:${source.plan_id || "_"}:${source.label}`;
    const existing = byKey.get(key);
    if (existing) {
      existing.available_units += source.available_units;
    } else {
      byKey.set(key, { ...source });
    }
  }
  return Array.from(byKey.values()).sort((a, b) => (rank[a.source] ?? 99) - (rank[b.source] ?? 99));
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
