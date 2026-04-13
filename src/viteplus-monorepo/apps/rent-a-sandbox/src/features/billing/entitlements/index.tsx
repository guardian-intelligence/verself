import { Fragment } from "react";
import type {
  EntitlementBucketSection,
  EntitlementProductSection,
  EntitlementSlot,
  EntitlementSourceTotal,
  EntitlementsView,
} from "~/server-fns/api";
import { formatLedgerAmount } from "~/lib/format";

// Display-only presentation of the honest slot model. The server still returns
// the four-scope slot tree (account → product → bucket → sku); this view
// flattens it into one receipt per concrete SKU and pulls the account-scoped
// purchase balance out as a top-line "Account Balance" header.
//
// Pooling note: bucket- and product-scoped sources (e.g. a Hobby plan's
// per-bucket allotment) appear in every SKU row under that bucket/product. That
// is deliberately optimistic — each row answers "how much juice could this SKU
// draw, ignoring contention with siblings" — and matches how the funder
// actually drains: most-specific scope first, then pooled scopes. Do not try to
// "fix" the apparent over-count; that's the whole reason the slot tree exists
// behind this view. The account balance row is the only scope that is NOT
// pooled into per-SKU receipts, because the subheading tells the customer it
// only drains after everything else.

export function EntitlementsPanel({ view }: { view: EntitlementsView }) {
  // The hidden `data-test-available-units` attribute is a deliberate
  // cross-slot sum used by the e2e harness's `readBalance()` for *relative*
  // comparisons (started_balance vs finished_balance). It double-counts in
  // every honest sense — different slots cover different scopes the funder
  // drains in a fixed order — and is never displayed to users. Do not render
  // this value, and do not "fix" it into a top-line total. The whole point of
  // the slot model is that no honest cross-slot sum exists; this attribute
  // earns its dishonesty by being monotonic-under-debit, hidden, and
  // test-only.
  const totalAvailableForTests = collectVisibleSlots(view).reduce(
    (acc, slot) => acc + slot.available_units,
    0,
  );

  const accountBalanceUnits = (view.universal?.sources ?? [])
    .filter((source) => source.source === "purchase")
    .reduce((acc, source) => acc + source.available_units, 0);

  const products = view.products ?? [];

  return (
    <div
      className="space-y-10"
      data-testid="entitlements-view"
      data-test-available-units={totalAvailableForTests}
    >
      <AccountBalanceHeader units={accountBalanceUnits} />
      {products.map((section) => (
        <ProductSection key={section.product_id} section={section} />
      ))}
    </div>
  );
}

function AccountBalanceHeader({ units }: { units: number }) {
  return (
    <section className="space-y-1" data-testid="entitlements-account-balance">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">Account Balance</div>
      <div
        className="font-mono text-3xl font-semibold tabular-nums"
        data-testid="account-balance-value"
      >
        {formatLedgerAmount(units)}
      </div>
      <p className="text-sm text-muted-foreground max-w-xl">
        Account balance is only deducted after all other credit sources for a product have been
        used.
      </p>
    </section>
  );
}

function ProductSection({ section }: { section: EntitlementProductSection }) {
  const productSources = section.product_slot?.sources ?? [];
  const buckets = section.buckets ?? [];
  // Flatten bucket → sku into a single receipt table. Bucket and product
  // sources are merged into each SKU row's receipt (see pooling note above).
  const rows = buckets.flatMap((bucket) => {
    const bucketSources = bucket.bucket_slot?.sources ?? [];
    return bucket.sku_slots.map((sku) => ({
      bucket,
      sku,
      sources: combineSources([...sku.sources, ...bucketSources, ...productSources]),
    }));
  });

  return (
    <section
      className="space-y-3"
      data-testid={`entitlements-product-${section.product_id}`}
    >
      <h2 className="text-lg font-semibold">{section.display_name}</h2>
      <div className="border border-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="text-left px-4 py-2 font-medium">SKU</th>
              <th className="text-right px-4 py-2 font-medium">Available</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {rows.length === 0 ? (
              <tr>
                <td className="px-4 py-3 text-muted-foreground" colSpan={2}>
                  No SKUs configured for this product.
                </td>
              </tr>
            ) : (
              rows.map(({ bucket, sku, sources }) => (
                <tr
                  key={`${bucket.bucket_id}:${sku.sku_id}`}
                  data-testid={`entitlements-sku-${section.product_id}:${bucket.bucket_id}:${sku.sku_id}`}
                  data-bucket-id={bucket.bucket_id}
                  data-sku-id={sku.sku_id}
                >
                  <td className="px-4 py-3 align-top">
                    <div className="font-medium">{displaySKUName(sku)}</div>
                    <div className="text-xs uppercase tracking-wide text-muted-foreground mt-0.5">
                      {bucket.display_name}
                    </div>
                  </td>
                  <td className="px-4 py-3 align-top">
                    <ReceiptCell sources={sources} />
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function ReceiptCell({ sources }: { sources: EntitlementSourceTotal[] }) {
  const total = sources.reduce((acc, source) => acc + source.available_units, 0);
  if (sources.length === 0 || total === 0) {
    return (
      <div className="text-right font-mono tabular-nums text-muted-foreground">$0.00</div>
    );
  }
  return (
    <div className="ml-auto w-max min-w-[14rem]">
      <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-1 text-xs">
        {sources.map((source) => (
          <Fragment key={`${source.source}:${source.plan_id || "_"}`}>
            <dt
              className="text-muted-foreground"
              data-source={source.source}
              data-plan-id={source.plan_id || undefined}
            >
              {source.label}
            </dt>
            <dd className="font-mono tabular-nums text-right text-foreground">
              {formatLedgerAmount(source.available_units)}
            </dd>
          </Fragment>
        ))}
      </dl>
      <div className="mt-2 border-t-2 border-foreground/70 pt-1.5 grid grid-cols-[1fr_auto] gap-x-6 text-sm font-bold">
        <div>Total</div>
        <div
          className="font-mono tabular-nums text-right"
          data-testid="slot-available"
        >
          {formatLedgerAmount(total)}
        </div>
      </div>
    </div>
  );
}

function combineSources(sources: EntitlementSourceTotal[]): EntitlementSourceTotal[] {
  const rank: Record<string, number> = {
    free_tier: 0,
    subscription: 1,
    promo: 2,
    refund: 3,
    purchase: 4,
  };
  const byKey = new Map<string, EntitlementSourceTotal>();
  for (const source of sources) {
    if (source.available_units === 0) continue;
    // Account-scoped top-ups surface only via the dedicated Account Balance
    // header; don't double-count them in per-SKU receipts.
    if (source.source === "purchase") continue;
    const key = `${source.source}:${source.plan_id || "_"}:${source.label}`;
    const existing = byKey.get(key);
    if (existing) {
      existing.available_units += source.available_units;
    } else {
      byKey.set(key, { ...source });
    }
  }
  return Array.from(byKey.values()).sort(
    (a, b) => (rank[a.source] ?? 99) - (rank[b.source] ?? 99),
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

function displaySKUName(slot: EntitlementSlot): string {
  if (slot.sku_display && slot.sku_display !== slot.sku_id) {
    return slot.sku_display;
  }
  return slot.coverage_label || slot.sku_id;
}

// Re-export so tests can import bucket identifiers symbolically if needed;
// intentionally retained as a side-effect-free hook.
export type { EntitlementBucketSection };
