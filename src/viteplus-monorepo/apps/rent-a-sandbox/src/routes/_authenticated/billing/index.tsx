import { Fragment, useMemo, useState } from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ErrorCallout } from "~/components/error-callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import { BillingFlashNotice, ContractStatusPill } from "~/features/billing/components";
import { EntitlementsPanel } from "~/features/billing/entitlements";
import { parseFlashSearch, projectFlashIntent } from "~/features/billing/flash";
import {
  useCancelContractMutation,
  useCreatePortalSessionMutation,
} from "~/features/billing/mutations";
import { loadBillingPage } from "~/features/billing/queries";
import type { CreditsProductState } from "~/features/billing/state";
import { useBillingAccountWithStatement } from "~/features/billing/use-billing-account";
import {
  formatDateTimeMillisUTC,
  formatDateUTC,
  formatInteger,
  formatLedgerAmount,
  formatLedgerAmountPrecise,
  formatLedgerRate,
} from "~/lib/format";
import type { EntitlementSourceTotal, Statement } from "~/lib/sandbox-rental-api";

type StatementLineItem = Statement["line_items"][number];
type LineItemDrainKey =
  | "free_tier_units"
  | "contract_units"
  | "purchase_units"
  | "promo_units"
  | "refund_units";
type DrainSourceKind = "free_tier" | "contract" | "purchase" | "promo" | "refund";

interface DrainSourceSpec {
  readonly key: LineItemDrainKey;
  readonly source: DrainSourceKind;
  readonly fallbackLabel: string;
}

// The ORDER of this array is the ORDER drains render inside each usage line.
// It matches the funder's consumption order so the UI column reads top-down
// the same way the ledger drains.
const DRAIN_SOURCES: readonly DrainSourceSpec[] = [
  { key: "free_tier_units", source: "free_tier", fallbackLabel: "Free tier" },
  { key: "contract_units", source: "contract", fallbackLabel: "Contract" },
  { key: "purchase_units", source: "purchase", fallbackLabel: "Account balance" },
  { key: "promo_units", source: "promo", fallbackLabel: "Promo" },
  { key: "refund_units", source: "refund", fallbackLabel: "Refund" },
];

const SANDBOX_PRODUCT_ID = "sandbox";

export const Route = createFileRoute("/_authenticated/billing/")({
  validateSearch: parseFlashSearch,
  loader: ({ context }) => loadBillingPage(context.queryClient, context.auth),
  component: BillingPage,
});

function BillingPage() {
  const flashSearch = Route.useSearch();
  const flashIntent = useMemo(() => projectFlashIntent(flashSearch), [flashSearch]);
  const initial = Route.useLoaderData();
  const { account, snapshot } = useBillingAccountWithStatement({
    initialPlans: initial.plans,
    initialContracts: initial.contracts,
    initialEntitlements: initial.entitlements,
    initialStatement: initial.statement,
  });
  const [cancelTarget, setCancelTarget] = useState<string | null>(null);
  const portalMutation = useCreatePortalSessionMutation();
  const cancelMutation = useCancelContractMutation();

  const contractRows = snapshot.contracts.contracts ?? [];
  const statement = snapshot.statement;
  const sandboxCredits = account.credits.byProduct.get(SANDBOX_PRODUCT_ID);

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold">Billing</h1>
        <div className="flex flex-wrap gap-3">
          {contractRows.length > 0 ? (
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
            Choose Plan
          </Link>
          <Link
            to="/billing/credits"
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
          >
            Buy Credits
          </Link>
        </div>
      </div>

      <BillingFlashNotice intent={flashIntent} />

      {portalMutation.error ? (
        <ErrorCallout error={portalMutation.error} title="Billing portal failed" />
      ) : null}

      {cancelMutation.error ? (
        <ErrorCallout error={cancelMutation.error} title="Contract cancellation failed" />
      ) : null}

      <EntitlementsPanel view={snapshot.entitlements} />

      {statement ? (
        <StatementPreview statement={statement} sandboxCredits={sandboxCredits} />
      ) : null}

      <section className="space-y-3">
        <h2 className="text-lg font-semibold mb-3">Contracts</h2>
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
              {contractRows.length > 0 ? (
                contractRows.map((contract) => {
                  const canCancel =
                    contract.status === "active" &&
                    contract.entitlement_state !== "closed" &&
                    contract.entitlement_state !== "voided";
                  const isCancelTarget = cancelTarget === contract.contract_id;

                  return (
                    <tr
                      key={contract.contract_id}
                      data-testid={`contract-row-${contract.contract_id}`}
                    >
                      <td className="px-4 py-2 font-medium">{contract.plan_id}</td>
                      <td className="px-4 py-2">
                        <ContractStatusPill status={contract.status} />
                      </td>
                      <td className="px-4 py-2">{contract.payment_state}</td>
                      <td className="px-4 py-2">{contract.entitlement_state}</td>
                      <td className="px-4 py-2">{contract.cadence_kind}</td>
                      <td className="px-4 py-2 text-muted-foreground">
                        {contract.ends_at ? formatDateUTC(contract.ends_at) : "--"}
                      </td>
                      <td className="px-4 py-2 text-right">
                        {canCancel ? (
                          isCancelTarget ? (
                            <div className="flex justify-end gap-2">
                              <button
                                type="button"
                                onClick={() => {
                                  cancelMutation.mutate(contract.contract_id, {
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
                                Keep Contract
                              </button>
                            </div>
                          ) : (
                            <button
                              type="button"
                              data-testid={`cancel-contract-${contract.contract_id}`}
                              onClick={() => setCancelTarget(contract.contract_id)}
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
                  title="No active contracts"
                  description="Choose a plan to start receiving bucketed usage credits."
                />
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function StatementPreview({
  statement,
  sandboxCredits,
}: {
  statement: Statement;
  sandboxCredits: CreditsProductState | undefined;
}) {
  const lineItems = statement.line_items;
  const grandTotal = statement.totals.total_due_units;

  return (
    <section className="space-y-3" data-testid="statement-usage">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Usage</h2>
        <p className="text-sm text-muted-foreground">
          Current billing cycle started at {formatDateTimeMillisUTC(statement.period_start)}
        </p>
      </div>

      <div className="border border-border rounded-lg overflow-hidden text-sm">
        <div className="bg-muted/50 px-4 py-2 flex items-baseline justify-between font-medium">
          <span>SKU</span>
          <span>Usage</span>
        </div>
        {lineItems.length > 0 ? (
          // Single CSS grid across every line item so that the quantity column
          // (col 2), the "=" / "−" column (col 4), and the amount column (col 5)
          // all align across rows. Col 3 is a 1fr spacer that absorbs slack to
          // the LEFT of the equal sign.
          <div className="grid grid-cols-[auto_auto_minmax(1rem,1fr)_auto_auto] items-baseline">
            {lineItems.map((line) => (
              <UsageLineRow
                key={`${line.product_id}:${line.plan_id}:${line.bucket_id}:${line.sku_id}:${line.pricing_phase}:${line.unit_rate}`}
                line={line}
                sandboxCredits={sandboxCredits}
              />
            ))}
            <div
              className="col-span-5 mx-4 mt-2 mb-4 border-t-2 border-foreground/80 pt-2 flex items-baseline justify-between text-base font-bold"
              data-testid="statement-grand-total"
            >
              <span>Amount Owed</span>
              <span className="font-mono tabular-nums">
                {formatLedgerAmountPrecise(grandTotal)}
              </span>
            </div>
          </div>
        ) : (
          <div className="px-4 py-6 text-center text-muted-foreground">
            <div className="font-medium">No usage yet</div>
            <div className="mt-1 text-xs">Usage will appear after windows settle.</div>
          </div>
        )}
      </div>
    </section>
  );
}

function UsageLineRow({
  line,
  sandboxCredits,
}: {
  line: StatementLineItem;
  sandboxCredits: CreditsProductState | undefined;
}) {
  const availableSources = sandboxCredits?.bySKU.get(line.sku_id) ?? [];
  const drains = DRAIN_SOURCES.map((source) => {
    const amount = line[source.key];
    const match = findRemainingSource(availableSources, source.source, line.plan_id);
    return {
      key: source.key,
      source: source.source,
      fallbackLabel: source.fallbackLabel,
      amount,
      label: match ? match.label : source.fallbackLabel,
      planId: match ? match.plan_id : undefined,
      remainingUnits: match ? match.available_units : 0,
    };
  }).filter((row) => row.amount > 0 || row.remainingUnits > 0);
  const hasReserved = line.reserved_units > 0;
  const skuTitle = `${line.bucket_display_name} — ${line.sku_display_name}`;
  const totalRows = 1 + drains.length + (hasReserved ? 1 : 0);

  const quantityText = formatQuantity(line.quantity, line.quantity_unit);
  const rateText = formatLedgerRate(line.unit_rate, line.quantity_unit);
  const chargeText = formatLedgerAmountPrecise(line.charge_units);

  const lastDrainIdx = hasReserved ? -1 : drains.length - 1;

  return (
    <Fragment>
      <div
        className="self-start break-words px-4 pt-4 pb-4 font-medium"
        style={{ gridRow: `span ${totalRows}`, gridColumn: 1 }}
        data-testid={`usage-line-${line.bucket_id}:${line.sku_id}`}
        data-bucket-id={line.bucket_id}
        data-sku-id={line.sku_id}
      >
        {skuTitle}
      </div>

      <div className="pt-4 font-mono tabular-nums">{quantityText}</div>
      <div className="pt-4 pl-2 font-mono tabular-nums whitespace-nowrap">@ {rateText}</div>
      <div className="pt-4 pl-2 font-mono tabular-nums">+</div>
      <div className="pt-4 pl-2 pr-4 font-mono tabular-nums">{chargeText}</div>

      {drains.map((drain, idx) => {
        const pb = idx === lastDrainIdx ? "pb-4" : "";
        const showRemaining = drain.source !== "purchase";
        return (
          <Fragment key={drain.key}>
            <div className={`pt-1 ${pb} text-muted-foreground`} data-drain-source={drain.key}>
              {drain.label}
              {showRemaining ? (
                <span
                  className="ml-1 text-xs"
                  data-source={drain.source}
                  data-plan-id={drain.planId}
                >
                  ({formatLedgerAmount(drain.remainingUnits)} remaining)
                </span>
              ) : null}
            </div>
            <div className={`pt-1 ${pb}`} />
            <div className={`pt-1 ${pb} pl-2 font-mono tabular-nums text-foreground`}>−</div>
            <div className={`pt-1 ${pb} pl-2 pr-4 font-mono tabular-nums text-foreground`}>
              {formatLedgerAmountPrecise(drain.amount)}
            </div>
          </Fragment>
        );
      })}

      {hasReserved ? (
        <Fragment>
          <div className="pt-1 pb-4 italic text-muted-foreground">Reserved (in-flight)</div>
          <div className="pt-1 pb-4" />
          <div className="pt-1 pb-4 pl-2 font-mono tabular-nums italic text-muted-foreground">
            −
          </div>
          <div className="pt-1 pb-4 pl-2 pr-4 font-mono tabular-nums italic text-muted-foreground">
            {formatLedgerAmountPrecise(line.reserved_units)}
          </div>
        </Fragment>
      ) : null}
    </Fragment>
  );
}

function findRemainingSource(
  sources: readonly EntitlementSourceTotal[],
  drainSource: DrainSourceKind,
  linePlanID: string,
): EntitlementSourceTotal | undefined {
  for (const source of sources) {
    if (source.source !== drainSource) continue;
    if (drainSource === "contract" && source.plan_id !== linePlanID) continue;
    return source;
  }
  return undefined;
}

function formatQuantity(value: number, quantityUnit: string) {
  const amount = Number.isInteger(value)
    ? formatInteger(value)
    : value.toLocaleString(undefined, { maximumFractionDigits: 5 });
  return `${amount} ${formatQuantityUnit(quantityUnit, value)}`;
}

function formatQuantityUnit(quantityUnit: string, quantity: number) {
  if (quantity === 1) return quantityUnit;
  switch (quantityUnit) {
    case "GiB-ms":
      return "GiB-ms";
    case "vCPU-ms":
      return "vCPU-ms";
    case "GiB-second":
      return "GiB-seconds";
    case "vCPU-second":
      return "vCPU-seconds";
    default:
      return quantityUnit;
  }
}
