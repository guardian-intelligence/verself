import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuth } from "@forge-metal/auth-web/isomorphic";
import { getBalance, getGrants, getStatement, getSubscriptions } from "~/server-fns/api";

function billingQueryKey<TParts extends readonly unknown[]>(
  auth: AuthenticatedAuth,
  ...parts: TParts
) {
  return authQueryKey(auth, "billing", ...parts);
}

export const balanceQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "balance"),
    queryFn: () => getBalance(),
  });

export const subscriptionsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "subscriptions"),
    queryFn: () => getSubscriptions(),
  });

export const activeGrantsQuery = (auth: AuthenticatedAuth) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "grants", { active: true }),
    queryFn: () => getGrants({ data: { active: true } }),
  });

export const statementQuery = (auth: AuthenticatedAuth, productId: string) =>
  queryOptions({
    queryKey: billingQueryKey(auth, "statement", { productId }),
    queryFn: () => getStatement({ data: { productId } }),
  });

export async function loadBalance(queryClient: QueryClient, auth: AuthenticatedAuth) {
  return queryClient.ensureQueryData(balanceQuery(auth));
}

export async function loadBillingPage(queryClient: QueryClient, auth: AuthenticatedAuth) {
  const [balance, subscriptions, grants] = await Promise.all([
    queryClient.ensureQueryData(balanceQuery(auth)),
    queryClient.ensureQueryData(subscriptionsQuery(auth)),
    queryClient.ensureQueryData(activeGrantsQuery(auth)),
  ]);

  return {
    balance,
    subscriptions,
    grants,
  };
}
