import type {
  BillingPlan,
  Contract,
  ContractsResponse,
  EntitlementSourceTotal,
  EntitlementsView,
  PlansResponse,
  Statement,
} from "~/lib/sandbox-rental-api";

// Billing state collapses the four boundary-parsed objects the billing-service
// returns (plans, contracts, entitlements, statement) into a small set of
// discriminated unions the UI can render off exhaustively. Every route that
// touches billing — subscribe, billing index, jobs gate — consumes these
// types instead of walking raw wire shapes or fanning out booleans from
// Contract.pending_change_type.

// --------------------------------------------------------------------------
// Public types
// --------------------------------------------------------------------------

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
  // Per-product pools so the usage table can answer "how much of this
  // product's bucket remains" without re-walking the entitlement tree.
  // Purchase (account balance) is intentionally omitted from byProduct —
  // it only surfaces via accountBalance.
  readonly byProduct: ReadonlyMap<string, CreditsProductState>;
  readonly accountBalance: AccountBalance;
};

export type CreditsProductState = {
  readonly productId: string;
  readonly availableUnits: number;
  readonly pendingUnits: number;
  // Flattened source list per SKU in consumption order
  // (free_tier → contract → promo → refund). Keyed by sku_id.
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

export function deriveBillingAccount(snapshot: BillingSnapshot): BillingAccount {
  const plans = snapshot.plans.plans ?? [];
  const contracts = snapshot.contracts.contracts ?? [];
  const credits = buildCreditsState(snapshot.entitlements);

  // Single active sandbox contract. Multi-product support would pick the
  // sandbox row explicitly; only the sandbox product is sold today.
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
    // A contract whose plan_id is missing from the catalog is a data
    // integrity break, not a rendering edge case — fail loud.
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
        // Clicking the current plan while a downgrade is pending resumes it;
        // billing-service treats a same-plan "start" as a resume affordance.
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
  return plans.map((plan) => derivePlanCard(account, plan));
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

  // Exhausted = nothing left to draw anywhere: no universal slot, no
  // account balance, no product bucket.
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

// combineSources merges bucket- and product-scoped entitlements into a flat
// list per SKU in draining order. Purchase (account balance) is excluded —
// it surfaces only via AccountBalance.
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
// Misc helpers
// --------------------------------------------------------------------------

function parseIsoDate(value: string | undefined | null): Date | null {
  if (!value) return null;
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed : null;
}

// Missing switch arms for any BillingAccount or PlanCardState discriminant
// become compile errors at every consumer.
export function assertUnreachable(value: never): never {
  throw new Error(`unreachable: ${JSON.stringify(value)}`);
}
