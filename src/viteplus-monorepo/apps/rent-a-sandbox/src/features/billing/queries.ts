import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { authQueryKey, type AuthenticatedAuthState } from "@forge-metal/auth-web";
import { getBalance, getGrants, getSubscriptions } from "~/server-fns/api";

function billingQueryKey<TParts extends readonly unknown[]>(
  authState: AuthenticatedAuthState,
  ...parts: TParts
) {
  return authQueryKey(authState, "billing", ...parts);
}

export const balanceQuery = (authState: AuthenticatedAuthState) =>
  queryOptions({
    queryKey: billingQueryKey(authState, "balance"),
    queryFn: () => getBalance(),
  });

export const subscriptionsQuery = (authState: AuthenticatedAuthState) =>
  queryOptions({
    queryKey: billingQueryKey(authState, "subscriptions"),
    queryFn: () => getSubscriptions(),
  });

export const activeGrantsQuery = (authState: AuthenticatedAuthState) =>
  queryOptions({
    queryKey: billingQueryKey(authState, "grants", { active: true }),
    queryFn: () => getGrants({ data: { active: true } }),
  });

export async function loadBalance(queryClient: QueryClient, authState: AuthenticatedAuthState) {
  return queryClient.ensureQueryData(balanceQuery(authState));
}

export async function loadBillingPage(queryClient: QueryClient, authState: AuthenticatedAuthState) {
  const [balance, subscriptions, grants] = await Promise.all([
    queryClient.ensureQueryData(balanceQuery(authState)),
    queryClient.ensureQueryData(subscriptionsQuery(authState)),
    queryClient.ensureQueryData(activeGrantsQuery(authState)),
  ]);

  return {
    balance,
    subscriptions,
    grants,
  };
}
