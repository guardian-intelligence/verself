import { Fragment, useMemo, useState } from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Check, Rocket, Sparkles } from "lucide-react";
import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-metal/ui/components/ui/table";
import { ErrorCallout } from "~/components/error-callout";
import { BillingFlashNotice } from "~/features/billing/components";
import { EntitlementsPanel } from "~/features/billing/entitlements";
import { parseFlashSearch, projectFlashIntent } from "~/features/billing/flash";
import {
  useCancelContractMutation,
  useCreatePortalSessionMutation,
} from "~/features/billing/mutations";
import { loadBillingPage } from "~/features/billing/queries";
import type { BillingAccount, CreditsProductState } from "~/features/billing/state";
import { useBillingAccountWithStatement } from "~/features/billing/use-billing-account";
import {
  formatDateTimeMillisUTC,
  formatDateUTC,
  formatInteger,
  formatLedgerAmount,
  formatLedgerAmountPrecise,
  formatLedgerRate,
} from "~/lib/format";
import type {
  BillingPlan,
  Contract,
  EntitlementSourceTotal,
  Statement,
} from "~/lib/sandbox-rental-api";

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

export const Route = createFileRoute("/_shell/_authenticated/settings/billing/")({
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
  const portalMutation = useCreatePortalSessionMutation();

  const statement = snapshot.statement;
  const sandboxCredits = account.credits.byProduct.get(SANDBOX_PRODUCT_ID);
  const hasContract = account.kind !== "no_contract";

  return (
    <PageSections>
      <PageSection>
        <PlanHero account={account} statement={statement} />
        <div className="flex flex-wrap gap-2">
          {hasContract ? (
            <Button
              type="button"
              variant="outline"
              className="w-full sm:w-auto"
              onClick={() => portalMutation.mutate()}
              disabled={portalMutation.isPending}
            >
              {portalMutation.isPending ? "Opening…" : "Manage billing"}
            </Button>
          ) : null}
          <Button
            variant="default"
            className="w-full sm:w-auto"
            render={<Link to="/settings/billing/credits" />}
          >
            Buy credits
          </Button>
        </div>
        <BillingFlashNotice intent={flashIntent} />
        {portalMutation.error ? (
          <ErrorCallout error={portalMutation.error} title="Billing portal failed" />
        ) : null}
      </PageSection>

      <PageSection>
        <EntitlementsPanel view={snapshot.entitlements} />
      </PageSection>

      <PageSection>
        <ContractsSection contracts={snapshot.contracts.contracts ?? []} />
      </PageSection>

      {statement ? (
        <PageSection>
          <StatementPreview statement={statement} sandboxCredits={sandboxCredits} />
        </PageSection>
      ) : null}
    </PageSections>
  );
}

function ContractsSection({ contracts }: { contracts: readonly Contract[] }) {
  const [cancelTarget, setCancelTarget] = useState<Contract | null>(null);
  const cancelMutation = useCancelContractMutation();

  const activeCancelTarget = cancelTarget && canCancelContract(cancelTarget) ? cancelTarget : null;

  async function confirmCancellation() {
    if (!activeCancelTarget) return;
    await cancelMutation.mutateAsync(activeCancelTarget.contract_id);
    setCancelTarget(null);
  }

  return (
    <Fragment>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Contracts</SectionTitle>
          <SectionDescription>
            Active subscription contracts for this organization.
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>
      <div className="overflow-hidden rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Plan</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Entitlement</TableHead>
              <TableHead>Payment</TableHead>
              <TableHead>Renews</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {contracts.length > 0 ? (
              contracts.map((contract) => (
                <TableRow
                  key={contract.contract_id}
                  data-testid={`contract-row-${contract.contract_id}`}
                >
                  <TableCell>
                    <div className="space-y-0.5">
                      <div className="font-medium">{contract.plan_id}</div>
                      <div className="text-muted-foreground">{contract.product_id}</div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={statusBadgeVariant(contract.status)}>{contract.status}</Badge>
                  </TableCell>
                  <TableCell>{contract.entitlement_state}</TableCell>
                  <TableCell>{contract.payment_state}</TableCell>
                  <TableCell>{formatContractRenewal(contract)}</TableCell>
                  <TableCell className="text-right">
                    {canCancelContract(contract) ? (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => setCancelTarget(contract)}
                      >
                        Cancel
                      </Button>
                    ) : contract.status === "cancel_scheduled" ||
                      contract.pending_change_type === "cancel" ? (
                      <Badge variant="warning">Cancel scheduled</Badge>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                  No contracts
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      {activeCancelTarget ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-0.5">
              <div className="font-medium">Cancel {activeCancelTarget.plan_id}</div>
              <p className="text-muted-foreground">
                Cancellation is scheduled for the end of the current billing phase.
              </p>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <Button type="button" variant="outline" onClick={() => setCancelTarget(null)}>
                Keep plan
              </Button>
              <Button
                type="button"
                variant="destructive"
                onClick={() => void confirmCancellation()}
                disabled={cancelMutation.isPending}
              >
                {cancelMutation.isPending ? "Cancelling…" : "Confirm Cancellation"}
              </Button>
            </div>
          </div>
          {cancelMutation.error ? (
            <div className="mt-3">
              <ErrorCallout error={cancelMutation.error} title="Cancellation failed" />
            </div>
          ) : null}
        </div>
      ) : null}
    </Fragment>
  );
}

function canCancelContract(contract: Contract): boolean {
  return (
    contract.status === "active" &&
    contract.pending_change_type !== "cancel" &&
    contract.pending_change_id === undefined
  );
}

function statusBadgeVariant(status: Contract["status"]) {
  switch (status) {
    case "active":
      return "success";
    case "cancel_scheduled":
      return "warning";
    default:
      return "outline";
  }
}

function formatContractRenewal(contract: Contract): string {
  const phaseEnd = contract.phase_end;
  if (!phaseEnd) return "End of current phase";
  return formatDateUTC(phaseEnd);
}

// PlanHero is the top-of-page summary block. Paid plans render as
// "<plan> plan / price / auto-renew line" with an outline "Adjust plan"
// action; free accounts render a short feature checklist with a filled
// "Upgrade plan" action. Everything keys off BillingAccount discriminants
// so the three paid kinds (active / pending_downgrade / pending_cancel)
// render their own renewal line without branching at the call site.
function PlanHero({
  account,
  statement,
}: {
  account: BillingAccount;
  statement: Statement | null;
}) {
  if (account.kind === "no_contract") {
    return (
      <section
        className="flex flex-col gap-4 rounded-lg border p-5 sm:flex-row sm:items-start"
        data-testid="plan-hero"
        data-account-kind="no_contract"
      >
        <PlanIcon variant="free" />
        <div className="min-w-0 flex-1 space-y-3">
          <div className="space-y-0.5">
            <h3 className="text-base font-semibold">Free plan</h3>
            <p className="text-sm text-muted-foreground">Try Console</p>
          </div>
          <ul className="space-y-1.5 text-sm text-muted-foreground">
            <FreeFeature>Run isolated sandboxes on bare metal</FreeFeature>
            <FreeFeature>Pay-as-you-go vCPU, memory, and disk metering</FreeFeature>
            <FreeFeature>Upgrade for monthly credit grants and priority lanes</FreeFeature>
          </ul>
        </div>
        <Button
          data-testid="plan-hero-cta"
          className="w-full sm:w-auto"
          render={<Link to="/settings/billing/subscribe" />}
        >
          Upgrade plan
        </Button>
      </section>
    );
  }

  return (
    <section
      className="flex flex-col gap-4 rounded-lg border p-5 sm:flex-row sm:items-start"
      data-testid="plan-hero"
      data-account-kind={account.kind}
      data-plan-id={account.plan.plan_id}
    >
      <PlanIcon variant="paid" />
      <div className="min-w-0 flex-1 space-y-1">
        <h3 className="text-base font-semibold">{account.plan.display_name} plan</h3>
        <p className="text-sm text-muted-foreground">
          {formatMonthlyPrice(account.plan.monthly_amount_cents)}
        </p>
        <p className="text-sm text-muted-foreground" data-testid="plan-hero-renewal">
          {renewalLineFor(account, statement)}
        </p>
      </div>
      <Button
        variant="outline"
        data-testid="plan-hero-cta"
        className="w-full sm:w-auto"
        render={<Link to="/settings/billing/subscribe" />}
      >
        Adjust plan
      </Button>
    </section>
  );
}

function PlanIcon({ variant }: { variant: "free" | "paid" }) {
  const Icon = variant === "free" ? Sparkles : Rocket;
  return (
    <div className="flex size-10 shrink-0 items-center justify-center rounded-full border">
      <Icon className="size-5" />
    </div>
  );
}

function FreeFeature({ children }: { children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2">
      <Check className="size-4 shrink-0 translate-y-0.5" />
      <span>{children}</span>
    </li>
  );
}

function renewalLineFor(
  account: Exclude<BillingAccount, { kind: "no_contract" }>,
  statement: Statement | null,
): string {
  switch (account.kind) {
    case "active": {
      const renews = account.contract.phase_end || statement?.period_end;
      return renews
        ? `Your subscription will auto-renew on ${formatDateUTC(renews)}.`
        : "Your subscription will auto-renew at the end of this cycle.";
    }
    case "pending_downgrade":
      return account.effectiveAt
        ? `Downgrades to ${account.target.display_name} on ${formatDateUTC(account.effectiveAt.toISOString())}.`
        : `Downgrades to ${account.target.display_name} at the end of this cycle.`;
    case "pending_cancel":
      return account.effectiveAt
        ? `Cancels on ${formatDateUTC(account.effectiveAt.toISOString())}.`
        : "Cancels at the end of this cycle.";
  }
}

function formatMonthlyPrice(cents: BillingPlan["monthly_amount_cents"]): string {
  if (!Number.isFinite(cents)) return "Custom pricing";
  return `$${(cents / 100).toFixed(2)} / month`;
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
    <Fragment>
      <SectionHeader>
        <SectionHeaderContent>
          <SectionTitle>Usage</SectionTitle>
          <SectionDescription>
            Current cycle started {formatDateTimeMillisUTC(statement.period_start)}
          </SectionDescription>
        </SectionHeaderContent>
      </SectionHeader>
      {/* The statement body is the one part of the billing page where
          receipt-style monospaced numerics genuinely help — charges,
          drains, and the grand total line up across rows. Chrome
          around it (borders, heading) stays default shadcn. */}
      <div className="overflow-hidden rounded-md border text-sm" data-testid="statement-usage">
        {lineItems.length > 0 ? (
          <Fragment>
            <div className="hidden items-baseline justify-between border-b bg-muted/50 px-4 py-2 text-xs font-medium xl:flex">
              <span>SKU</span>
              <span>Usage</span>
            </div>
            {/* Single CSS grid across every wide line item so that the quantity column
                (col 2), the "=" / "−" column (col 4), and the amount column (col 5)
                all align across rows. Narrower settings columns switch to receipt
                rows; otherwise the rate and charge columns collide. */}
            <div className="xl:grid xl:grid-cols-[minmax(14rem,1fr)_auto_minmax(1rem,0.5fr)_auto_auto] xl:items-baseline">
              {lineItems.map((line) => (
                <UsageLineRow
                  key={`${line.product_id}:${line.plan_id}:${line.bucket_id}:${line.sku_id}:${line.pricing_phase}:${line.unit_rate}`}
                  line={line}
                  sandboxCredits={sandboxCredits}
                />
              ))}
              <div
                className="mx-4 mt-2 mb-4 flex items-baseline justify-between border-t-2 border-foreground/80 pt-2 text-base font-bold xl:col-span-5"
                data-testid="statement-grand-total"
              >
                <span>Amount Owed</span>
                <span className="font-mono tabular-nums">
                  {formatLedgerAmountPrecise(grandTotal)}
                </span>
              </div>
            </div>
          </Fragment>
        ) : (
          <div className="px-4 py-6 text-center text-muted-foreground">
            <div className="font-medium">No usage yet</div>
            <div className="mt-1 text-xs">Usage will appear after windows settle.</div>
          </div>
        )}
      </div>
    </Fragment>
  );
}

function UsageLineRow({
  line,
  sandboxCredits,
}: {
  line: StatementLineItem;
  sandboxCredits: CreditsProductState | undefined;
}) {
  const view = buildUsageLineView(line, sandboxCredits);

  return (
    <Fragment>
      <CompactUsageLineRow view={view} />
      <WideUsageLineRow view={view} />
    </Fragment>
  );
}

interface UsageLineView {
  readonly line: StatementLineItem;
  readonly skuTitle: string;
  readonly totalRows: number;
  readonly quantityText: string;
  readonly rateText: string;
  readonly chargeText: string;
  readonly reservedText: string;
  readonly hasReserved: boolean;
  readonly drains: readonly UsageDrainView[];
}

interface UsageDrainView {
  readonly key: LineItemDrainKey;
  readonly source: DrainSourceKind;
  readonly amount: number;
  readonly amountText: string;
  readonly label: string;
  readonly planId: string | undefined;
  readonly remainingUnits: number;
  readonly remainingText: string;
}

function buildUsageLineView(
  line: StatementLineItem,
  sandboxCredits: CreditsProductState | undefined,
): UsageLineView {
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
      amountText: formatLedgerAmountPrecise(amount),
      remainingText: formatLedgerAmount(match ? match.available_units : 0),
    };
  }).filter((row) => row.amount > 0 || row.remainingUnits > 0);
  const hasReserved = line.reserved_units > 0;
  const skuTitle = `${line.bucket_display_name} — ${line.sku_display_name}`;
  const totalRows = 1 + drains.length + (hasReserved ? 1 : 0);

  const quantityText = formatQuantity(line.quantity, line.quantity_unit);
  const rateText = formatLedgerRate(line.unit_rate, line.quantity_unit);
  const chargeText = formatLedgerAmountPrecise(line.charge_units);

  return {
    line,
    skuTitle,
    totalRows,
    quantityText,
    rateText,
    chargeText,
    reservedText: formatLedgerAmountPrecise(line.reserved_units),
    hasReserved,
    drains,
  };
}

function CompactUsageLineRow({ view }: { view: UsageLineView }) {
  return (
    <div
      className="border-b px-4 py-4 xl:hidden"
      data-usage-layout="compact"
      data-bucket-id={view.line.bucket_id}
      data-sku-id={view.line.sku_id}
    >
      <div className="font-medium">{view.skuTitle}</div>
      <div className="mt-3 grid gap-x-3 gap-y-1 font-mono tabular-nums sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className="min-w-0 break-words">
          {view.quantityText} @ {view.rateText}
        </div>
        <div className="whitespace-nowrap text-right">= {view.chargeText}</div>
      </div>

      {view.drains.length > 0 || view.hasReserved ? (
        <div className="mt-2 space-y-1.5">
          {view.drains.map((drain) => {
            const showRemaining = drain.source !== "purchase";
            return (
              <div
                key={drain.key}
                className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3"
                data-drain-source={drain.key}
              >
                <div className="min-w-0 text-muted-foreground">
                  {drain.label}
                  {showRemaining ? (
                    <span
                      className="ml-1 text-xs"
                      data-source={drain.source}
                      data-plan-id={drain.planId}
                    >
                      ({drain.remainingText} remaining)
                    </span>
                  ) : null}
                </div>
                <div className="whitespace-nowrap font-mono tabular-nums text-foreground">
                  − {drain.amountText}
                </div>
              </div>
            );
          })}

          {view.hasReserved ? (
            <div className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-3 italic text-muted-foreground">
              <div className="min-w-0">Reserved (in-flight)</div>
              <div className="whitespace-nowrap font-mono tabular-nums">− {view.reservedText}</div>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function WideUsageLineRow({ view }: { view: UsageLineView }) {
  const lastDrainIdx = view.hasReserved ? -1 : view.drains.length - 1;

  return (
    <div className="hidden xl:contents" data-usage-layout="wide">
      <div
        className="self-start break-words px-4 pt-4 pb-4 font-medium"
        style={{ gridRow: `span ${view.totalRows}`, gridColumn: 1 }}
        data-testid={`usage-line-${view.line.bucket_id}:${view.line.sku_id}`}
        data-bucket-id={view.line.bucket_id}
        data-sku-id={view.line.sku_id}
      >
        {view.skuTitle}
      </div>

      <div className="pt-4 font-mono whitespace-nowrap tabular-nums">{view.quantityText}</div>
      <div className="pt-4 pl-2 font-mono whitespace-nowrap tabular-nums">@ {view.rateText}</div>
      <div className="pt-4 pl-2 font-mono tabular-nums">=</div>
      <div className="pt-4 pl-2 pr-4 font-mono whitespace-nowrap tabular-nums">
        {view.chargeText}
      </div>

      {view.drains.map((drain, idx) => {
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
                  ({drain.remainingText} remaining)
                </span>
              ) : null}
            </div>
            <div className={`pt-1 ${pb}`} />
            <div className={`pt-1 ${pb} pl-2 font-mono tabular-nums text-foreground`}>−</div>
            <div className={`pt-1 ${pb} pl-2 pr-4 font-mono tabular-nums text-foreground`}>
              {drain.amountText}
            </div>
          </Fragment>
        );
      })}

      {view.hasReserved ? (
        <Fragment>
          <div className="pt-1 pb-4 italic text-muted-foreground">Reserved (in-flight)</div>
          <div className="pt-1 pb-4" />
          <div className="pt-1 pb-4 pl-2 font-mono tabular-nums italic text-muted-foreground">
            −
          </div>
          <div className="pt-1 pb-4 pl-2 pr-4 font-mono tabular-nums italic text-muted-foreground">
            {view.reservedText}
          </div>
        </Fragment>
      ) : null}
    </div>
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
