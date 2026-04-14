import { trace, type Attributes } from "@opentelemetry/api";
import type {
  BillingPlan,
  Contract,
  ContractsResponse,
  EntitlementSourceTotal,
  EntitlementsView,
  PlansResponse,
  Statement,
} from "~/lib/sandbox-rental-api";

// --------------------------------------------------------------------------
// Public types
// --------------------------------------------------------------------------

// Every branch that represents "the customer is paying us" carries the same
// CreditsState so downstream views (usage table, jobs CTA, credits header)
// never have to do their own bookkeeping.
export type BillingAccount =
  | { readonly kind: "no_contract"; readonly credits: CreditsState }
  | {
      readonly kind: "active";
      readonly contract: Contract;
      readonly plan: BillingPlan;
      readonly credits: CreditsState;
    }
  | {
      readonly kind: "pending_downgrade";
      readonly contract: Contract;
      readonly plan: BillingPlan;
      readonly target: BillingPlan;
      readonly effectiveAt: Date | null;
      readonly credits: CreditsState;
    }
  | {
      readonly kind: "pending_cancel";
      readonly contract: Contract;
      readonly plan: BillingPlan;
      readonly effectiveAt: Date | null;
      readonly credits: CreditsState;
    };

export type CreditsState = {
  readonly kind: "available" | "exhausted";
  readonly availableUnits: number;
  readonly pendingUnits: number;
  // Per-product pools so the usage table can answer "how much of this product's
  // bucket remains" without re-walking the entitlement tree. Purchase (account
  // balance) is intentionally omitted from byProduct — it only surfaces via
  // accountBalance above the table. See features/billing/entitlements.
  readonly byProduct: ReadonlyMap<string, CreditsProductState>;
  readonly accountBalance: AccountBalance;
};

export type CreditsProductState = {
  readonly productId: string;
  readonly availableUnits: number;
  readonly pendingUnits: number;
  // Flattened source list per SKU in consumption order (free_tier → contract →
  // promo → refund → purchase). Key: sku_id. This is the same shape the old
  // SKURemainingLookup produced; consumers that need drain annotations read
  // from here.
  readonly bySKU: ReadonlyMap<string, readonly EntitlementSourceTotal[]>;
};

export type AccountBalance = {
  readonly availableUnits: number;
  readonly pendingUnits: number;
};

export type PlanCardState =
  | { readonly kind: "fresh_start"; readonly plan: BillingPlan }
  | { readonly kind: "current"; readonly plan: BillingPlan }
  | { readonly kind: "current_resumable"; readonly plan: BillingPlan }
  | {
      readonly kind: "upgrade_target";
      readonly plan: BillingPlan;
      readonly fromPlan: BillingPlan;
      readonly deltaCents: number;
    }
  | {
      readonly kind: "downgrade_target";
      readonly plan: BillingPlan;
      readonly fromPlan: BillingPlan;
    }
  | {
      readonly kind: "scheduled_downgrade";
      readonly plan: BillingPlan;
      readonly effectiveAt: Date | null;
    }
  | { readonly kind: "locked_by_pending_change"; readonly plan: BillingPlan };

export type PlanCardIntent =
  | { readonly kind: "start"; readonly planId: string }
  | {
      readonly kind: "upgrade" | "downgrade" | "resume";
      readonly contractId: string;
      readonly targetPlanId: string;
    }
  | { readonly kind: "disabled" };

export type BillingSnapshot = {
  readonly plans: PlansResponse;
  readonly contracts: ContractsResponse;
  readonly entitlements: EntitlementsView;
  readonly statement: Statement | null;
};

// --------------------------------------------------------------------------
// Derivation
// --------------------------------------------------------------------------

const SANDBOX_PRODUCT_ID = "sandbox";

// Pure derivation. Wrapped in a billing.account.derive span so each SSR
// observation shows up in ClickHouse with the observed account.kind and
// credits.kind attributes. Client-side re-derivation is a no-op at the OTel
// layer (no browser provider registered) but still returns the correct value.
export function deriveBillingAccount(snapshot: BillingSnapshot): BillingAccount {
  const tracer = trace.getTracer("rent-a-sandbox");
  return tracer.startActiveSpan("billing.account.derive", (span) => {
    try {
      const account = buildAccount(snapshot);
      span.setAttributes(accountSpanAttributes(account));
      return account;
    } finally {
      span.end();
    }
  });
}

function buildAccount(snapshot: BillingSnapshot): BillingAccount {
  const plans = snapshot.plans.plans ?? [];
  const contracts = snapshot.contracts.contracts ?? [];
  const credits = buildCreditsState(snapshot.entitlements);

  // Single active sandbox contract. Multiple-product support would pick the
  // sandbox row explicitly; we only ship the sandbox product today.
  const activeContract = contracts.find(
    (c) =>
      c.product_id === SANDBOX_PRODUCT_ID &&
      (c.status === "active" || c.status === "cancel_scheduled") &&
      c.plan_id !== "",
  );
  if (!activeContract) {
    return { kind: "no_contract", credits };
  }

  const plan = plans.find((p) => p.plan_id === activeContract.plan_id);
  if (!plan) {
    // Contract references a plan the catalog doesn't know about — treat as
    // no_contract for UX purposes, but keep the raw contract around if we ever
    // add a diagnostic surface. Loud failure matches CLAUDE.md guidance.
    throw new BillingAccountError(
      `contract ${activeContract.contract_id} references unknown plan_id=${activeContract.plan_id}`,
    );
  }

  const effectiveAt = parseIsoDate(activeContract.pending_change_effective_at);

  if (
    activeContract.pending_change_type === "cancel" ||
    activeContract.status === "cancel_scheduled"
  ) {
    return {
      kind: "pending_cancel",
      contract: activeContract,
      plan,
      effectiveAt,
      credits,
    };
  }

  if (activeContract.pending_change_type === "downgrade") {
    const targetId = activeContract.pending_change_target_plan_id;
    const target = targetId ? (plans.find((p) => p.plan_id === targetId) ?? plan) : plan;
    return {
      kind: "pending_downgrade",
      contract: activeContract,
      plan,
      target,
      effectiveAt,
      credits,
    };
  }

  return { kind: "active", contract: activeContract, plan, credits };
}

export class BillingAccountError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "BillingAccountError";
  }
}

// --------------------------------------------------------------------------
// Plan card derivation
// --------------------------------------------------------------------------

export function derivePlanCard(account: BillingAccount, plan: BillingPlan): PlanCardState {
  switch (account.kind) {
    case "no_contract":
      return { kind: "fresh_start", plan };

    case "active": {
      if (plan.plan_id === account.plan.plan_id) {
        return { kind: "current", plan };
      }
      if (plan.monthly_amount_cents > account.plan.monthly_amount_cents) {
        return {
          kind: "upgrade_target",
          plan,
          fromPlan: account.plan,
          deltaCents: plan.monthly_amount_cents - account.plan.monthly_amount_cents,
        };
      }
      return { kind: "downgrade_target", plan, fromPlan: account.plan };
    }

    case "pending_downgrade": {
      if (plan.plan_id === account.plan.plan_id) {
        // Clicking the current plan while a downgrade is pending resumes it —
        // same-plan "start" is the billing-service's documented resume affordance.
        return { kind: "current_resumable", plan };
      }
      if (plan.plan_id === account.target.plan_id) {
        return { kind: "scheduled_downgrade", plan, effectiveAt: account.effectiveAt };
      }
      return { kind: "locked_by_pending_change", plan };
    }

    case "pending_cancel": {
      if (plan.plan_id === account.plan.plan_id) {
        return { kind: "current_resumable", plan };
      }
      return { kind: "locked_by_pending_change", plan };
    }
  }
}

export function deriveAllPlanCards(
  account: BillingAccount,
  plans: readonly BillingPlan[],
): readonly PlanCardState[] {
  const tracer = trace.getTracer("rent-a-sandbox");
  return tracer.startActiveSpan("billing.plan_cards.derive", (span) => {
    try {
      const cards = plans.map((plan) => derivePlanCard(account, plan));
      const kinds = cards.map((c) => c.kind);
      span.setAttributes({
        "billing.account.kind": account.kind,
        "billing.plan_cards.count": cards.length,
        "billing.plan_cards.kinds": kinds.join(","),
      });
      return cards;
    } finally {
      span.end();
    }
  });
}

export function intentFor(card: PlanCardState, account: BillingAccount): PlanCardIntent {
  switch (card.kind) {
    case "fresh_start":
      return { kind: "start", planId: card.plan.plan_id };
    case "upgrade_target":
      return requireContract(account, {
        kind: "upgrade",
        targetPlanId: card.plan.plan_id,
      });
    case "downgrade_target":
      return requireContract(account, {
        kind: "downgrade",
        targetPlanId: card.plan.plan_id,
      });
    case "current_resumable":
      return requireContract(account, {
        kind: "resume",
        targetPlanId: card.plan.plan_id,
      });
    case "current":
    case "scheduled_downgrade":
    case "locked_by_pending_change":
      return { kind: "disabled" };
  }
}

function requireContract(
  account: BillingAccount,
  partial: { kind: "upgrade" | "downgrade" | "resume"; targetPlanId: string },
): PlanCardIntent {
  if (account.kind === "no_contract" || !("contract" in account) || !account.contract.contract_id) {
    return { kind: "disabled" };
  }
  return {
    kind: partial.kind,
    contractId: account.contract.contract_id,
    targetPlanId: partial.targetPlanId,
  };
}

// --------------------------------------------------------------------------
// Credits aggregation
// --------------------------------------------------------------------------

function buildCreditsState(view: EntitlementsView): CreditsState {
  const accountBalance = buildAccountBalance(view);
  const byProduct = new Map<string, CreditsProductState>();

  for (const product of view.products ?? []) {
    byProduct.set(product.product_id, buildProductCredits(product));
  }

  // "exhausted" = no product bucket can draw AND account balance can't draw
  // AND the universal slot has nothing left. This matches the old nested
  // entitlements.products.every() check in jobs/index.tsx, collapsed to one
  // computation on the account shape.
  const universalAvailable = view.universal.available_units;
  const anyProductAvailable = Array.from(byProduct.values()).some((p) => p.availableUnits > 0);
  const exhausted =
    universalAvailable <= 0 && accountBalance.availableUnits <= 0 && !anyProductAvailable;

  return {
    kind: exhausted ? "exhausted" : "available",
    availableUnits: sumCreditsAvailable(universalAvailable, byProduct, accountBalance),
    pendingUnits:
      view.universal.pending_units +
      Array.from(byProduct.values()).reduce((acc, p) => acc + p.pendingUnits, 0) +
      accountBalance.pendingUnits,
    byProduct,
    accountBalance,
  };
}

function sumCreditsAvailable(
  universal: number,
  byProduct: ReadonlyMap<string, CreditsProductState>,
  accountBalance: AccountBalance,
): number {
  let total = universal + accountBalance.availableUnits;
  for (const product of byProduct.values()) {
    total += product.availableUnits;
  }
  return total;
}

function buildAccountBalance(view: EntitlementsView): AccountBalance {
  const purchaseSources = (view.universal?.sources ?? []).filter((s) => s.source === "purchase");
  return {
    availableUnits: purchaseSources.reduce((acc, s) => acc + s.available_units, 0),
    pendingUnits: purchaseSources.reduce((acc, s) => acc + s.pending_units, 0),
  };
}

function buildProductCredits(
  product: NonNullable<EntitlementsView["products"]>[number],
): CreditsProductState {
  const productSlotAvailable = product.product_slot?.available_units ?? 0;
  const productSlotPending = product.product_slot?.pending_units ?? 0;
  const productSources = product.product_slot?.sources ?? [];
  const bySKU = new Map<string, readonly EntitlementSourceTotal[]>();

  let availableUnits = productSlotAvailable;
  let pendingUnits = productSlotPending;

  for (const bucket of product.buckets ?? []) {
    availableUnits += bucket.bucket_slot?.available_units ?? 0;
    pendingUnits += bucket.bucket_slot?.pending_units ?? 0;
    const bucketSources = bucket.bucket_slot?.sources ?? [];

    for (const sku of bucket.sku_slots ?? []) {
      availableUnits += sku.available_units;
      pendingUnits += sku.pending_units;
      bySKU.set(sku.sku_id, combineSources([...sku.sources, ...bucketSources, ...productSources]));
    }
  }

  return {
    productId: product.product_id,
    availableUnits,
    pendingUnits,
    bySKU,
  };
}

// Lifted verbatim from features/billing/entitlements so derive is the single
// entry point for credits aggregation. The old module now re-exports from here.
function combineSources(
  sources: readonly EntitlementSourceTotal[],
): readonly EntitlementSourceTotal[] {
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

// --------------------------------------------------------------------------
// Span helpers
// --------------------------------------------------------------------------

function accountSpanAttributes(account: BillingAccount): Attributes {
  const base: Attributes = {
    "billing.account.kind": account.kind,
    "billing.credits.kind": account.credits.kind,
    "billing.credits.available_units": account.credits.availableUnits,
    "billing.credits.pending_units": account.credits.pendingUnits,
    "billing.credits.product_count": account.credits.byProduct.size,
    "billing.credits.account_balance_available_units":
      account.credits.accountBalance.availableUnits,
  };
  if ("plan" in account) {
    base["billing.active_plan_id"] = account.plan.plan_id;
    base["billing.active_plan_tier"] = account.plan.tier;
  }
  if (account.kind === "pending_downgrade") {
    base["billing.pending_target_plan_id"] = account.target.plan_id;
    if (account.effectiveAt) {
      base["billing.pending_effective_at"] = account.effectiveAt.toISOString();
    }
  }
  if (account.kind === "pending_cancel" && account.effectiveAt) {
    base["billing.pending_effective_at"] = account.effectiveAt.toISOString();
  }
  return base;
}

export function planCardActionAttributes(
  card: PlanCardState,
  intent: PlanCardIntent,
  account: BillingAccount,
): Attributes {
  return {
    "billing.account.kind": account.kind,
    "billing.plan_card.kind": card.kind,
    "billing.plan_card.plan_id": card.plan.plan_id,
    "billing.plan_card.plan_tier": card.plan.tier,
    "billing.plan_card.intent": intent.kind,
  };
}

// --------------------------------------------------------------------------
// Misc helpers
// --------------------------------------------------------------------------

function parseIsoDate(value: string | undefined | null): Date | null {
  if (!value) return null;
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed : null;
}

// Exhaustiveness assertion — any missing switch arm becomes a compile error
// at every consumer of a BillingAccount / PlanCardState discriminant.
export function assertUnreachable(value: never): never {
  throw new Error(`unreachable: ${JSON.stringify(value)}`);
}
