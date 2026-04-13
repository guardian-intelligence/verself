import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@forge-metal/ui/components/ui/card";
import { Separator } from "@forge-metal/ui/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import type {
  EntitlementBucketSection,
  EntitlementProductSection,
  EntitlementSlot,
  EntitlementSourceTotal,
  EntitlementsView,
} from "~/server-fns/api";
import { formatDateUTC, formatLedgerAmount } from "~/lib/format";

const combinedCaption =
  "Each row only covers what its label says. Balances on different rows can't be combined.";

export function EntitlementsPanel({ view }: { view: EntitlementsView }) {
  const productSections = view.products ?? [];
  // The hidden `data-test-available-units` attribute is a deliberate
  // cross-slot sum used by e2e harness `readBalance()` for *relative*
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

  return (
    <div
      className="space-y-8"
      data-testid="entitlements-view"
      data-test-available-units={totalAvailableForTests}
    >
      <UniversalCard slot={view.universal} />
      {productSections.length > 0 ? (
        <div className="space-y-6">
          {productSections.map((section) => (
            <ProductCard key={section.product_id} section={section} />
          ))}
        </div>
      ) : null}
    </div>
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

function UniversalCard({ slot }: { slot: EntitlementSlot }) {
  return (
    <Card data-testid="entitlements-universal">
      <CardHeader>
        <CardTitle>Usable anywhere</CardTitle>
        <CardDescription>
          Account-scoped credit. This row covers any product, bucket, or SKU.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="border border-border rounded-md">
          <Table>
            <TableHeader>
              <TableRow>
                <SlotColumnHeads />
              </TableRow>
            </TableHeader>
            <TableBody>
              <SlotRow slot={slot} />
            </TableBody>
          </Table>
        </div>
        <Separator />
        <p className="text-xs text-muted-foreground" data-testid="entitlements-caption">
          {combinedCaption}
        </p>
      </CardContent>
    </Card>
  );
}

function ProductCard({ section }: { section: EntitlementProductSection }) {
  const buckets = section.buckets ?? [];
  return (
    <Card data-testid={`entitlements-product-${section.product_id}`}>
      <CardHeader>
        <CardTitle>{section.display_name}</CardTitle>
        <CardDescription>
          Coverage rows for {section.display_name}. Rows never combine — each line is a standalone
          coverage statement.
        </CardDescription>
      </CardHeader>
      <div className="border-t border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <SlotColumnHeads />
            </TableRow>
          </TableHeader>
          <TableBody>
            {section.product_slot ? <SlotRow slot={section.product_slot} /> : null}
            {buckets.map((bucket) => (
              <BucketRows key={bucket.bucket_id} bucket={bucket} />
            ))}
          </TableBody>
        </Table>
      </div>
    </Card>
  );
}

function SlotColumnHeads() {
  return (
    <>
      <TableHead>Covers</TableHead>
      <TableHead>Period started with</TableHead>
      <TableHead className="text-right">Spent</TableHead>
      <TableHead className="text-right">Available</TableHead>
    </>
  );
}

function BucketRows({ bucket }: { bucket: EntitlementBucketSection }) {
  return (
    <>
      <TableRow
        data-testid={`entitlements-bucket-${bucket.bucket_id}`}
        className="bg-muted/40 hover:bg-muted/40"
      >
        <TableCell
          colSpan={4}
          className="py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground"
        >
          {bucket.display_name}
        </TableCell>
      </TableRow>
      {bucket.bucket_slot ? <SlotRow slot={bucket.bucket_slot} /> : null}
      {bucket.sku_slots.map((slot) => (
        <SlotRow key={slot.sku_id} slot={slot} />
      ))}
    </>
  );
}

function SlotRow({ slot }: { slot: EntitlementSlot }) {
  return (
    <TableRow data-testid={`entitlements-slot-${slotKey(slot)}`}>
      <TableCell className="font-medium align-top">{slot.coverage_label}</TableCell>
      <TableCell className="align-top">
        <PeriodStartCell slot={slot} />
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums align-top">
        <span data-testid="slot-spent">{formatLedgerAmount(slot.spent_units)}</span>
      </TableCell>
      <TableCell className="text-right align-top">
        <AvailableCell slot={slot} />
      </TableCell>
    </TableRow>
  );
}

function PeriodStartCell({ slot }: { slot: EntitlementSlot }) {
  if (slot.period_start_units === 0) {
    return <span className="text-muted-foreground">—</span>;
  }
  const periodSources = slot.sources.filter((source) => source.period_start_units > 0);
  return (
    <div className="space-y-1">
      <div className="font-mono tabular-nums text-sm font-medium" data-testid="slot-period-start">
        {formatLedgerAmount(slot.period_start_units)}
      </div>
      <SourceTuple sources={periodSources} field="period_start_units" />
    </div>
  );
}

function AvailableCell({ slot }: { slot: EntitlementSlot }) {
  if (slot.available_units === 0 && slot.pending_units === 0) {
    return <span className="font-mono tabular-nums text-muted-foreground">$0.00</span>;
  }
  return (
    <div className="space-y-1">
      <div className="font-mono tabular-nums text-sm font-medium" data-testid="slot-available">
        {formatLedgerAmount(slot.available_units)}
        {slot.pending_units > 0 ? (
          <span className="ml-1 text-xs text-muted-foreground font-normal">
            (+ {formatLedgerAmount(slot.pending_units)} pending)
          </span>
        ) : null}
      </div>
      <SourceTuple sources={slot.sources} field="available_units" />
    </div>
  );
}

function SourceTuple({
  sources,
  field,
}: {
  sources: EntitlementSourceTotal[];
  field: "available_units" | "period_start_units";
}) {
  const visible = sources.filter((source) => source[field] > 0);
  if (visible.length === 0) return null;
  return (
    <ul className="text-xs text-muted-foreground space-y-0.5">
      {visible.map((source) => (
        <li
          key={`${source.source}:${source.plan_id || "_"}`}
          data-source={source.source}
          data-plan-id={source.plan_id || undefined}
        >
          <span className="font-mono tabular-nums text-foreground">
            {formatLedgerAmount(source[field])}
          </span>
          <span className="ml-1">[{sourceLabel(source)}]</span>
        </li>
      ))}
    </ul>
  );
}

function sourceLabel(source: EntitlementSourceTotal): string {
  if (source.inline_expires_at) {
    return `${source.label}, expires ${formatDateUTC(source.inline_expires_at)}`;
  }
  return source.label;
}

function slotKey(slot: EntitlementSlot): string {
  return [slot.scope_type, slot.product_id || "_", slot.bucket_id || "_", slot.sku_id || "_"].join(
    ":",
  );
}
