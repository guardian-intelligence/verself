import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@forge-metal/ui/components/ui/card";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Separator } from "@forge-metal/ui/components/ui/separator";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@forge-metal/ui/components/ui/tooltip";
import type {
  EntitlementBucketSection,
  EntitlementGrantEntry,
  EntitlementPool,
  EntitlementProductSection,
  EntitlementsView,
} from "~/server-fns/api";
import { formatDateUTC, formatLedgerAmount } from "~/lib/format";

const combinedCaption = "Balances can't be combined — each line is only usable for what it covers.";

export function EntitlementsPanel({ view }: { view: EntitlementsView }) {
  const universalPools = view.universal ?? [];
  const productSections = view.products ?? [];
  const isEmpty = universalPools.length === 0 && productSections.length === 0;
  // Hidden numeric anchor for e2e relative comparisons (started vs finished).
  // It is intentionally not displayed — the visible UI never sums across cells.
  const allEntries = [
    ...universalPools.flatMap((pool) => pool.entries),
    ...productSections.flatMap((section) => [
      ...(section.product_pools ?? []).flatMap((pool) => pool.entries),
      ...(section.buckets ?? []).flatMap((bucket) =>
        (bucket.pools ?? []).flatMap((pool) => pool.entries),
      ),
    ]),
  ];
  const totalAvailableForTests = allEntries.reduce((acc, entry) => acc + entry.available, 0);

  if (isEmpty) {
    return (
      <Card
        data-testid="entitlements-view"
        data-entitlements-empty="true"
        data-test-available-units={0}
      >
        <CardHeader>
          <CardTitle>No active credits</CardTitle>
          <CardDescription>
            Subscribe or purchase credits to see what's available where.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <TooltipProvider delayDuration={150}>
      <div
        className="space-y-8"
        data-testid="entitlements-view"
        data-test-available-units={totalAvailableForTests}
      >
        {universalPools.length > 0 ? <UniversalStrip pools={universalPools} /> : null}

        {productSections.length > 0 ? (
          <div className="space-y-6">
            {productSections.map((section) => (
              <ProductSection key={section.product_id} section={section} />
            ))}
          </div>
        ) : null}
      </div>
    </TooltipProvider>
  );
}

function UniversalStrip({ pools }: { pools: EntitlementPool[] }) {
  return (
    <Card data-testid="entitlements-universal">
      <CardHeader>
        <CardTitle>Usable anywhere</CardTitle>
        <CardDescription>
          Account-scoped credit. Each line is only usable for what it covers — never combine cells.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {pools.map((pool) => (
            <PoolCell key={poolKey(pool)} pool={pool} />
          ))}
        </div>
        <Separator />
        <p className="text-xs text-muted-foreground" data-testid="entitlements-caption">
          {combinedCaption}
        </p>
      </CardContent>
    </Card>
  );
}

function ProductSection({ section }: { section: EntitlementProductSection }) {
  const productPools = section.product_pools ?? [];
  const buckets = (section.buckets ?? []).filter((b) => (b.pools ?? []).length > 0);
  if (productPools.length === 0 && buckets.length === 0) return null;
  return (
    <Card data-testid={`entitlements-product-${section.product_id}`}>
      <CardHeader>
        <CardTitle>{section.display_name}</CardTitle>
        <CardDescription>
          Credits scoped to {section.display_name}. Each row is independent — read it by what it
          says it covers, never by adding it to another row.
        </CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        <div className="border-t border-border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Covers</TableHead>
                <TableHead>Source</TableHead>
                <TableHead className="text-right">Available</TableHead>
                <TableHead className="text-right">Pending</TableHead>
                <TableHead className="text-right">Next expiry</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {productPools.length > 0 ? (
                <>
                  <SectionHeaderRow label={`Anywhere in ${section.display_name}`} />
                  {productPools.map((pool) => (
                    <PoolRow key={poolKey(pool)} pool={pool} />
                  ))}
                </>
              ) : null}
              {buckets.map((bucket) => (
                <BucketRows key={bucket.bucket_id} bucket={bucket} />
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
}

function BucketRows({ bucket }: { bucket: EntitlementBucketSection }) {
  const pools = bucket.pools ?? [];
  if (pools.length === 0) return null;
  return (
    <>
      <SectionHeaderRow
        label={bucket.display_name}
        testid={`entitlements-bucket-${bucket.bucket_id}`}
      />
      {pools.map((pool) => (
        <PoolRow key={poolKey(pool)} pool={pool} />
      ))}
    </>
  );
}

function SectionHeaderRow({ label, testid }: { label: string; testid?: string }) {
  return (
    <TableRow data-testid={testid} className="bg-muted/40 hover:bg-muted/40">
      <TableCell
        colSpan={5}
        className="py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground"
      >
        {label}
      </TableCell>
    </TableRow>
  );
}

function PoolRow({ pool }: { pool: EntitlementPool }) {
  const totals = poolTotals(pool);
  const next = pool.entries[0];
  return (
    <TableRow data-testid={`entitlements-pool-${poolKey(pool)}`}>
      <TableCell className="font-medium">{pool.coverage_label}</TableCell>
      <TableCell>
        <SourceBadge label={pool.source_label} source={pool.source} />
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        <Tooltip>
          <TooltipTrigger asChild>
            <span data-testid="pool-available">{formatLedgerAmount(totals.available)}</span>
          </TooltipTrigger>
          <PoolEntriesTooltip entries={pool.entries} />
        </Tooltip>
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
        {formatLedgerAmount(totals.pending)}
      </TableCell>
      <TableCell className="text-right text-muted-foreground">
        {next?.expires_at ? formatDateUTC(next.expires_at) : "Never"}
      </TableCell>
    </TableRow>
  );
}

function PoolCell({ pool }: { pool: EntitlementPool }) {
  const totals = poolTotals(pool);
  const next = pool.entries[0];
  return (
    <div
      className="rounded-lg border border-border p-4 space-y-2"
      data-testid={`entitlements-pool-${poolKey(pool)}`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium text-muted-foreground">{pool.coverage_label}</span>
        <SourceBadge label={pool.source_label} source={pool.source} />
      </div>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className="text-2xl font-mono tabular-nums font-semibold"
            data-testid="pool-available"
          >
            {formatLedgerAmount(totals.available)}
          </div>
        </TooltipTrigger>
        <PoolEntriesTooltip entries={pool.entries} />
      </Tooltip>
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>
          {totals.pending > 0 ? `${formatLedgerAmount(totals.pending)} pending` : "\u00a0"}
        </span>
        <span>{next?.expires_at ? `Expires ${formatDateUTC(next.expires_at)}` : "No expiry"}</span>
      </div>
    </div>
  );
}

function PoolEntriesTooltip({ entries }: { entries: EntitlementGrantEntry[] }) {
  if (entries.length === 0) return <TooltipContent>No active grants</TooltipContent>;
  return (
    <TooltipContent className="max-w-xs space-y-1 text-xs">
      <div className="font-semibold">Grants in this cell, next-to-spend first</div>
      <ul className="space-y-1">
        {entries.map((entry) => (
          <li key={entry.grant_id} className="flex items-baseline justify-between gap-3">
            <span className="font-mono">{formatLedgerAmount(entry.available)}</span>
            <span className="text-muted-foreground">
              {entry.expires_at ? formatDateUTC(entry.expires_at) : "no expiry"}
            </span>
          </li>
        ))}
      </ul>
    </TooltipContent>
  );
}

function SourceBadge({ label, source }: { label: string; source: string }) {
  return (
    <Badge variant="secondary" data-source={source}>
      {label}
    </Badge>
  );
}

function poolTotals(pool: EntitlementPool) {
  let available = 0;
  let pending = 0;
  for (const entry of pool.entries) {
    available += entry.available;
    pending += entry.pending;
  }
  return { available, pending };
}

function poolKey(pool: EntitlementPool) {
  return [
    pool.scope_type,
    pool.product_id || "_",
    pool.bucket_id || "_",
    pool.sku_id || "_",
    pool.source,
  ].join(":");
}
