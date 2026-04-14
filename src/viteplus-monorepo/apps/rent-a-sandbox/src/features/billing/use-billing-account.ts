import { useMemo } from "react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { contractsQuery, entitlementsQuery, plansQuery, statementQuery } from "./queries";
import { deriveBillingAccount, type BillingAccount, type BillingSnapshot } from "./state";
import type {
  ContractsResponse,
  EntitlementsView,
  PlansResponse,
  Statement,
} from "~/lib/sandbox-rental-api";

export interface UseBillingAccountOptions {
  readonly initialPlans?: PlansResponse;
  readonly initialContracts?: ContractsResponse;
  readonly initialEntitlements?: EntitlementsView;
}

// Core billing snapshot: plans + contracts + entitlements. No statement —
// subscribe and jobs routes don't care about the current billing cycle's
// line items, and useSuspenseQuery can't conditionally disable a query.
// Callers that want the statement use useBillingAccountWithStatement below.
export function useBillingAccount(options: UseBillingAccountOptions = {}): {
  readonly account: BillingAccount;
  readonly snapshot: BillingSnapshot;
} {
  const auth = useSignedInAuth();
  const plans = useSuspenseQuery({
    ...plansQuery(auth),
    ...(options.initialPlans ? { initialData: options.initialPlans } : {}),
  }).data;
  const contracts = useSuspenseQuery({
    ...contractsQuery(auth),
    ...(options.initialContracts ? { initialData: options.initialContracts } : {}),
  }).data;
  const entitlements = useSuspenseQuery({
    ...entitlementsQuery(auth),
    ...(options.initialEntitlements ? { initialData: options.initialEntitlements } : {}),
  }).data;

  return useMemo(() => {
    const snapshot: BillingSnapshot = {
      plans,
      contracts,
      entitlements,
      statement: null,
    };
    return { account: deriveBillingAccount(snapshot), snapshot };
  }, [plans, contracts, entitlements]);
}

export interface UseBillingAccountWithStatementOptions extends UseBillingAccountOptions {
  readonly initialStatement?: Statement;
}

// Variant for routes that also render the current billing cycle's statement
// line items (the /billing index). Fetches all four queries unconditionally
// and derives the same BillingAccount shape plus a snapshot that carries
// the statement.
export function useBillingAccountWithStatement(
  options: UseBillingAccountWithStatementOptions = {},
): {
  readonly account: BillingAccount;
  readonly snapshot: BillingSnapshot;
} {
  const auth = useSignedInAuth();
  const plans = useSuspenseQuery({
    ...plansQuery(auth),
    ...(options.initialPlans ? { initialData: options.initialPlans } : {}),
  }).data;
  const contracts = useSuspenseQuery({
    ...contractsQuery(auth),
    ...(options.initialContracts ? { initialData: options.initialContracts } : {}),
  }).data;
  const entitlements = useSuspenseQuery({
    ...entitlementsQuery(auth),
    ...(options.initialEntitlements ? { initialData: options.initialEntitlements } : {}),
  }).data;
  const statement = useSuspenseQuery({
    ...statementQuery(auth, "sandbox"),
    ...(options.initialStatement ? { initialData: options.initialStatement } : {}),
  }).data;

  return useMemo(() => {
    const snapshot: BillingSnapshot = {
      plans,
      contracts,
      entitlements,
      statement,
    };
    return { account: deriveBillingAccount(snapshot), snapshot };
  }, [plans, contracts, entitlements, statement]);
}
