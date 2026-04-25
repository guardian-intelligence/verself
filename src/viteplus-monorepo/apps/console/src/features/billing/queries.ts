import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { trace } from "@opentelemetry/api";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getContracts, getEntitlements, getPlans, getStatement } from "~/server-fns/api";
import { deriveBillingAccount, type BillingSnapshot } from "./state";

function billingQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "billing", ...parts);
}

// Billing state crosses a cycle boundary on the server without the client
// knowing. Until we move to a live-synced projection (see the "Electric
// billing" follow-up — it requires a sandbox_rental projection table and
// sandbox-rental-service refresh endpoint), the cheapest fix for post-
// finalization staleness is to treat billing queries as always-stale on
// route re-entry and tab focus. Cost: one extra HTTP round trip on
// navigation back to a billing page. Benefit: the subscribe card grid
// reflects cycle-boundary plan activations within <1s of the user opening
// the page.
const BILLING_FRESHNESS = {
  staleTime: 0,
  refetchOnMount: "always" as const,
  refetchOnWindowFocus: true,
};

export const entitlementsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    ...BILLING_FRESHNESS,
    queryKey: billingQueryKey(auth, "entitlements"),
    queryFn: () => getEntitlements(),
  });

export const contractsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    ...BILLING_FRESHNESS,
    queryKey: billingQueryKey(auth, "contracts"),
    queryFn: () => getContracts(),
  });

export const plansQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    ...BILLING_FRESHNESS,
    queryKey: billingQueryKey(auth, "plans"),
    queryFn: () => getPlans(),
  });

export const statementQuery = (auth: AuthenticatedAuth, productId: string) =>
  queryOptions({
    ...BILLING_FRESHNESS,
    queryKey: billingQueryKey(auth, "statement", { productId }),
    queryFn: () => getStatement({ data: { productId } }),
  });

function logBillingPageLoadError(error: unknown) {
  const spanContext = trace.getActiveSpan()?.spanContext();
  const errorObject = error instanceof Error ? error : undefined;
  console.error(
    JSON.stringify({
      level: "error",
      msg: "billing page preload failed",
      trace_id: spanContext?.traceId ?? "",
      span_id: spanContext?.spanId ?? "",
      error_name: errorObject?.name ?? typeof error,
      error_message: errorObject?.message ?? String(error),
      error_stack: errorObject?.stack ?? "",
    }),
  );
}

export async function loadBillingPage(queryClient: QueryClient, auth: AuthenticatedAuth) {
  try {
    const [entitlements, contracts, plans, statement] = await Promise.all([
      queryClient.ensureQueryData(entitlementsQuery(auth)),
      queryClient.ensureQueryData(contractsQuery(auth)),
      queryClient.ensureQueryData(plansQuery(auth)),
      queryClient.ensureQueryData(statementQuery(auth, "sandbox")),
    ]);

    const snapshot: BillingSnapshot = { plans, contracts, entitlements, statement };
    const account = deriveBillingAccount(snapshot);

    return {
      entitlements,
      contracts,
      plans,
      statement,
      account,
    };
  } catch (error) {
    logBillingPageLoadError(error);
    throw error;
  }
}

export async function loadSubscribePage(queryClient: QueryClient, auth: AuthenticatedAuth) {
  try {
    const [plans, contracts, entitlements] = await Promise.all([
      queryClient.ensureQueryData(plansQuery(auth)),
      queryClient.ensureQueryData(contractsQuery(auth)),
      queryClient.ensureQueryData(entitlementsQuery(auth)),
    ]);
    const snapshot: BillingSnapshot = {
      plans,
      contracts,
      entitlements,
      statement: null,
    };
    const account = deriveBillingAccount(snapshot);
    return { plans, contracts, entitlements, account };
  } catch (error) {
    logBillingPageLoadError(error);
    throw error;
  }
}

export type LoadedBillingSnapshot = Awaited<ReturnType<typeof loadBillingPage>>;
export type LoadedSubscribeSnapshot = Awaited<ReturnType<typeof loadSubscribePage>>;

export { type BillingAccount } from "./state";
