import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { contractsQuery, entitlementsQuery, plansQuery } from "~/features/billing/queries";
import {
  deriveBillingAccount,
  type BillingAccount,
  type BillingSnapshot,
} from "~/features/billing/state";
import { getExecution } from "~/server-fns/api";
import { ensureOrNotFound } from "~/lib/query-loader";
import { isExecutionActiveStatus } from "./status";

// The executions page is gated on billing credits state, so we preload the
// three billing snapshot sources the selector needs. Statement is
// intentionally omitted — the credits_exhausted check derives from the
// entitlements tree, not from the current billing cycle's line items.
export async function loadExecutionsIndex(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
): Promise<{ account: BillingAccount }> {
  const [entitlements, contracts, plans] = await Promise.all([
    queryClient.ensureQueryData(entitlementsQuery(auth)),
    queryClient.ensureQueryData(contractsQuery(auth)),
    queryClient.ensureQueryData(plansQuery(auth)),
  ]);
  const snapshot: BillingSnapshot = {
    plans,
    contracts,
    entitlements,
    statement: null,
  };
  return { account: deriveBillingAccount(snapshot) };
}

export function executionQuery(auth: AuthenticatedAuth, executionId: string) {
  return queryOptions({
    queryKey: authQueryKey(auth, "executions", executionId),
    queryFn: () => getExecution({ data: { executionId } }),
    staleTime: 10_000,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return isExecutionActiveStatus(status) ? 2_000 : false;
    },
  });
}

export async function loadExecutionDetail(
  queryClient: QueryClient,
  auth: AuthenticatedAuth,
  executionId: string,
) {
  return ensureOrNotFound(queryClient, executionQuery(auth, executionId));
}
