import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { trace } from "@opentelemetry/api";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getContracts, getEntitlements, getPlans, getStatement } from "~/server-fns/api";

function billingQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "billing", ...parts);
}

export const entitlementsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "entitlements"),
    queryFn: () => getEntitlements(),
  });

export const contractsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "contracts"),
    queryFn: () => getContracts(),
  });

export const plansQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "plans"),
    queryFn: () => getPlans(),
  });

export const statementQuery = (auth: AuthenticatedAuth, productId: string) =>
  queryOptions({
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
    const [entitlements, contracts, statement] = await Promise.all([
      queryClient.ensureQueryData(entitlementsQuery(auth)),
      queryClient.ensureQueryData(contractsQuery(auth)),
      queryClient.ensureQueryData(statementQuery(auth, "sandbox")),
    ]);

    return {
      entitlements,
      contracts,
      statement,
    };
  } catch (error) {
    logBillingPageLoadError(error);
    throw error;
  }
}
