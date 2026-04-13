import { Fragment, useState } from "react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { Skeleton } from "@forge-metal/ui";
import { ErrorCallout } from "~/components/error-callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import { BillingFlashNotice, SubscriptionStatusPill } from "~/features/billing/components";
import { EntitlementsPanel } from "~/features/billing/entitlements";
import {
  useCancelSubscriptionMutation,
  useCreatePortalSessionMutation,
} from "~/features/billing/mutations";
import {
  entitlementsQuery,
  loadBillingPage,
  statementQuery,
  subscriptionsQuery,
} from "~/features/billing/queries";
import { parseBillingFlashSearch } from "~/features/billing/search";
import {
  formatDateUTC,
  formatInteger,
  formatLedgerAmountPrecise,
  formatLedgerRate,
} from "~/lib/format";
import type { Statement } from "~/server-fns/api";

type StatementLineItem = Statement["line_items"][number];
type LineItemDrainKey =
  | "free_tier_units"
  | "subscription_units"
  | "purchase_units"
  | "promo_units"
  | "refund_units";

const drainSources: Array<{ key: LineItemDrainKey; label: string }> = [
  { key: "free_tier_units", label: "Free tier" },
  { key: "subscription_units", label: "Subscription" },
  { key: "purchase_units", label: "Account balance" },
  { key: "promo_units", label: "Promo" },
  { key: "refund_units", label: "Refund" },
];

const sandboxProductID = "sandbox";

export const Route = createFileRoute("/_authenticated/billing/")({
  validateSearch: parseBillingFlashSearch,
  loader: ({ context }) => loadBillingPage(context.queryClient, context.auth),
  component: BillingPage,
});

function BillingPage() {
  const auth = useSignedInAuth();
  const flash = Route.useSearch();
  const [showStatement, setShowStatement] = useState(false);
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const entitlements = useSuspenseQuery(entitlementsQuery(auth)).data;
  const subscriptions = useSuspenseQuery(subscriptionsQuery(auth)).data;
  const statementResult = useQuery({
    ...statementQuery(auth, sandboxProductID),
    enabled: showStatement,
  });
  const portalMutation = useCreatePortalSessionMutation();
  const cancelMutation = useCancelSubscriptionMutation();

  const subscriptionRows = subscriptions.subscriptions ?? [];

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold">Billing</h1>
        <div className="flex flex-wrap gap-3">
          <button
            type="button"
            onClick={() => setShowStatement((visible) => !visible)}
            className="px-4 py-2 rounded-md border border-border hover:bg-accent text-sm"
          >
            {showStatement ? "Hide Usage" : "Show Usage"}
          </button>
          {subscriptionRows.length > 0 ? (
            <button
              type="button"
              onClick={() => portalMutation.mutate()}
              disabled={portalMutation.isPending}
              className="px-4 py-2 min-w-[128px] rounded-md border border-border hover:bg-accent text-sm disabled:opacity-50"
            >
              {portalMutation.isPending ? "Opening..." : "Manage Billing"}
            </button>
          ) : null}
          <Link
            to="/billing/subscribe"
            className="px-4 py-2 rounded-md border border-border hover:bg-accent text-sm"
          >
            Subscribe
          </Link>
          <Link
            to="/billing/credits"
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
          >
            Buy Credits
          </Link>
        </div>
      </div>

      <BillingFlashNotice {...flash} />

      {portalMutation.error ? (
        <ErrorCallout error={portalMutation.error} title="Billing portal failed" />
      ) : null}

      {cancelMutation.error ? (
        <ErrorCallout error={cancelMutation.error} title="Subscription cancellation failed" />
      ) : null}

      <EntitlementsPanel view={entitlements} />

      {showStatement ? (
        statementResult.error ? (
          <ErrorCallout error={statementResult.error} title="Statement failed" />
        ) : statementResult.data ? (
          <StatementPreview statement={statementResult.data} />
        ) : (
          <StatementPreviewSkeleton />
        )
      ) : null}

      <section className="space-y-3">
        <h2 className="text-lg font-semibold mb-3">Subscriptions</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Plan</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-left px-4 py-2 font-medium">Payment</th>
                <th className="text-left px-4 py-2 font-medium">Entitlement</th>
                <th className="text-left px-4 py-2 font-medium">Cadence</th>
                <th className="text-left px-4 py-2 font-medium">Period End</th>
                <th className="text-right px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {subscriptionRows.length > 0 ? (
                subscriptionRows.map((subscription) => {
                  const canCancel =
                    subscription.status !== "canceled" &&
                    subscription.entitlement_state !== "closed" &&
                    subscription.entitlement_state !== "voided";
                  const isCancelTarget = cancelTarget === subscription.subscription_id;

                  return (
                    <tr
                      key={subscription.subscription_id}
                      data-testid={`subscription-row-${subscription.plan_id}`}
                    >
                      <td className="px-4 py-2 font-medium">{subscription.plan_id}</td>
                      <td className="px-4 py-2">
                        <SubscriptionStatusPill status={subscription.status} />
                      </td>
                      <td className="px-4 py-2">{subscription.payment_state}</td>
                      <td className="px-4 py-2">{subscription.entitlement_state}</td>
                      <td className="px-4 py-2">{subscription.cadence}</td>
                      <td className="px-4 py-2 text-muted-foreground">
                        {subscription.current_period_end
                          ? formatDateUTC(subscription.current_period_end)
                          : "--"}
                      </td>
                      <td className="px-4 py-2 text-right">
                        {canCancel ? (
                          isCancelTarget ? (
                            <div className="flex justify-end gap-2">
                              <button
                                type="button"
                                onClick={() => {
                                  cancelMutation.mutate(subscription.subscription_id, {
                                    onSuccess: () => setCancelTarget(null),
                                  });
                                }}
                                disabled={cancelMutation.isPending}
                                className="px-3 py-1.5 rounded-md bg-destructive text-destructive-foreground hover:opacity-90 text-xs disabled:opacity-50"
                              >
                                {cancelMutation.isPending ? "Canceling..." : "Confirm Cancellation"}
                              </button>
                              <button
                                type="button"
                                onClick={() => setCancelTarget(null)}
                                disabled={cancelMutation.isPending}
                                className="px-3 py-1.5 rounded-md border border-border hover:bg-accent text-xs disabled:opacity-50"
                              >
                                Keep Subscription
                              </button>
                            </div>
                          ) : (
                            <button
                              type="button"
                              data-testid={`cancel-subscription-${subscription.plan_id}`}
                              onClick={() => setCancelTarget(subscription.subscription_id)}
                              disabled={cancelMutation.isPending}
                              className="px-3 py-1.5 rounded-md border border-border hover:bg-accent text-xs disabled:opacity-50"
                            >
                              Cancel
                            </button>
                          )
                        ) : (
                          <span className="text-muted-foreground">--</span>
                        )}
                      </td>
                    </tr>
                  );
                })
              ) : (
                <TableEmptyRow
                  colSpan={7}
                  title="No active subscriptions"
                  description="Subscribe to start receiving bucketed usage credits."
                />
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function StatementPreview({ statement }: { statement: Statement }) {
  const lineItems = statement.line_items ?? [];
  const grandTotal = statement.totals.total_due_units;

  return (
    <section className="space-y-3" data-testid="statement-usage">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Usage</h2>
        <p className="text-sm text-muted-foreground">
          {formatDateUTC(statement.period_start)} through {formatDateUTC(statement.period_end)}
        </p>
      </div>

      <div className="border border-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="text-left px-4 py-2 font-medium">SKU</th>
              <th className="text-right px-4 py-2 font-medium">Usage</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {lineItems.length > 0 ? (
              lineItems.map((line) => (
                <UsageLineRow
                  key={`${line.product_id}:${line.plan_id}:${line.bucket_id}:${line.sku_id}:${line.pricing_phase}:${line.unit_rate}`}
                  line={line}
                />
              ))
            ) : (
              <TableEmptyRow
                colSpan={2}
                title="No usage yet"
                description="Usage will appear after windows settle."
              />
            )}
            {lineItems.length > 0 ? (
              <tr>
                <td colSpan={2} className="px-4 pt-4 pb-3">
                  <div
                    className="border-t-2 border-foreground/80 pt-2 flex items-baseline justify-between text-base font-bold"
                    data-testid="statement-grand-total"
                  >
                    <span>Grand Total</span>
                    <span className="font-mono tabular-nums">
                      {formatLedgerAmountPrecise(grandTotal)}
                    </span>
                  </div>
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function UsageLineRow({ line }: { line: StatementLineItem }) {
  const drains = drainSources
    .map((source) => ({ ...source, amount: line[source.key] }))
    .filter((row) => row.amount > 0);
  const hasReserved = line.reserved_units > 0;
  const skuTitle = `${line.bucket_display_name} — ${line.sku_display_name}`;
  return (
    <tr
      data-testid={`usage-line-${line.bucket_id}:${line.sku_id}`}
      data-bucket-id={line.bucket_id}
      data-sku-id={line.sku_id}
    >
      <td className="px-4 py-4 align-top">
        <div className="font-medium break-words">{skuTitle}</div>
      </td>
      <td className="px-4 py-4 align-top">
        <div className="ml-auto w-max max-w-full font-mono tabular-nums text-sm">
          {formatUsageFormula(line)}
        </div>
        {drains.length > 0 || hasReserved ? (
          <dl className="ml-auto w-max max-w-full mt-3 min-w-[18rem] grid grid-cols-[1fr_auto] gap-x-6 gap-y-0.5 text-sm">
            {drains.map((drain) => (
              <Fragment key={drain.key}>
                <dt className="text-muted-foreground" data-drain-source={drain.key}>
                  {drain.label}
                </dt>
                <dd className="font-mono tabular-nums text-right text-foreground">
                  − {formatLedgerAmountPrecise(drain.amount)}
                </dd>
              </Fragment>
            ))}
            {hasReserved ? (
              <Fragment>
                <dt className="text-muted-foreground italic">Reserved (in-flight)</dt>
                <dd className="font-mono tabular-nums text-right text-muted-foreground italic">
                  − {formatLedgerAmountPrecise(line.reserved_units)}
                </dd>
              </Fragment>
            ) : null}
          </dl>
        ) : null}
      </td>
    </tr>
  );
}

function formatUsageFormula(line: StatementLineItem) {
  const quantityText = formatQuantity(line.quantity, line.quantity_unit);
  const rateText = formatLedgerRate(line.unit_rate, line.quantity_unit);
  const chargeText = formatLedgerAmountPrecise(line.charge_units);
  return (
    <>
      <span>{quantityText}</span>
      <span className="mx-1 text-muted-foreground">@</span>
      <span>{rateText}</span>
      <span className="mx-1 text-muted-foreground">=</span>
      <span className="font-semibold">{chargeText}</span>
    </>
  );
}

function StatementPreviewSkeleton() {
  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <Skeleton className="h-6 w-24" />
        <Skeleton className="h-4 w-56" />
      </div>
      <Skeleton className="h-48 w-full" />
    </section>
  );
}

function formatQuantity(value: number, quantityUnit: string) {
  const amount = Number.isInteger(value)
    ? formatInteger(value)
    : value.toLocaleString(undefined, { maximumFractionDigits: 3 });
  return `${amount} ${formatQuantityUnit(quantityUnit, value)}`;
}

function formatQuantityUnit(quantityUnit: string, quantity: number) {
  if (quantity === 1) return quantityUnit;
  switch (quantityUnit) {
    case "GiB-second":
      return "GiB-seconds";
    case "vCPU-second":
      return "vCPU-seconds";
    default:
      return quantityUnit;
  }
}
