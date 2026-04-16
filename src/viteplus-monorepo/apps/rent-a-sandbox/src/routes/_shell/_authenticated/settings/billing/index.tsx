import { Fragment, useMemo } from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Check, Rocket, Sparkles } from "lucide-react";
import { Button } from "@forge-metal/ui/components/ui/button";
import {
  PageSection,
  PageSections,
  SectionDescription,
  SectionHeader,
  SectionHeaderContent,
  SectionTitle,
} from "@forge-metal/ui/components/ui/page";
import { ErrorCallout } from "~/components/error-callout";
import { BillingFlashNotice } from "~/features/billing/components";
import { EntitlementsPanel } from "~/features/billing/entitlements";
import { parseFlashSearch, projectFlashIntent } from "~/features/billing/flash";
import { useCreatePortalSessionMutation } from "~/features/billing/mutations";
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
import type { BillingPlan, EntitlementSourceTotal, Statement } from "~/lib/sandbox-rental-api";

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
        <PlanHero account={account} />
        <div className="flex flex-wrap gap-2">
          {hasContract ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => portalMutation.mutate()}
              disabled={portalMutation.isPending}
            >
              {portalMutation.isPending ? "Opening…" : "Manage billing"}
            </Button>
          ) : null}
          <Button variant="default" render={<Link to="/settings/billing/credits" />}>
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

      {statement ? (
        <PageSection>
          <StatementPreview statement={statement} sandboxCredits={sandboxCredits} />
        </PageSection>
      ) : null}
    </PageSections>
  );
}

// PlanHero is the top-of-page summary block. Paid plans render as
// "<plan> plan / price / auto-renew line" with an outline "Adjust plan"
// action; free accounts render a short feature checklist with a filled
// "Upgrade plan" action. Everything keys off BillingAccount discriminants
// so the three paid kinds (active / pending_downgrade / pending_cancel)
// render their own renewal line without branching at the call site.
function PlanHero({ account }: { account: BillingAccount }) {
  if (account.kind === "no_contract") {
    return (
      <section
        className="flex items-start gap-4 rounded-lg border p-5"
        data-testid="plan-hero"
        data-account-kind="no_contract"
      >
        <PlanIcon variant="free" />
        <div className="min-w-0 flex-1 space-y-3">
          <div className="space-y-0.5">
            <h3 className="text-base font-semibold">Free plan</h3>
            <p className="text-sm text-muted-foreground">Try Rent-a-Sandbox</p>
          </div>
          <ul className="space-y-1.5 text-sm text-muted-foreground">
            <FreeFeature>Run isolated sandboxes on bare metal</FreeFeature>
            <FreeFeature>Pay-as-you-go vCPU, memory, and disk metering</FreeFeature>
            <FreeFeature>Upgrade for monthly credit grants and priority lanes</FreeFeature>
          </ul>
        </div>
        <Button data-testid="plan-hero-cta" render={<Link to="/settings/billing/subscribe" />}>
          Upgrade plan
        </Button>
      </section>
    );
  }

  return (
    <section
      className="flex items-start gap-4 rounded-lg border p-5"
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
          {renewalLineFor(account)}
        </p>
      </div>
      <Button
        variant="outline"
        data-testid="plan-hero-cta"
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
): string {
  switch (account.kind) {
    case "active": {
      const renews = account.contract.phase_end;
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
        <div className="flex items-baseline justify-between border-b bg-muted/50 px-4 py-2 text-xs font-medium">
          <span>SKU</span>
          <span>Usage</span>
        </div>
        {lineItems.length > 0 ? (
          // Single CSS grid across every line item so that the quantity column
          // (col 2), the "=" / "−" column (col 4), and the amount column (col 5)
          // all align across rows. Col 3 is a 1fr spacer that absorbs slack to
          // the LEFT of the equal sign.
          (<div className="grid grid-cols-[auto_auto_minmax(1rem,1fr)_auto_auto] items-baseline">
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
          </div>)
        ) : (
          <div className="px-4 py-6 text-center text-muted-foreground">
            <div className="font-medium">No usage yet</div>
            <div className="mt-1 text-xs">Usage will appear after windows settle.</div>
          </div>
        )}
      </div>
    </Fragment>
  )
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
