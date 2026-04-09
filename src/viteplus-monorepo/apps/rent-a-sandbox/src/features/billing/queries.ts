import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { getBalance, getGrants, getSubscriptions } from "~/server-fns/api";

export const balanceQuery = () =>
  queryOptions({
    queryKey: ["billing", "balance"] as const,
    queryFn: () => getBalance(),
  });

export const subscriptionsQuery = () =>
  queryOptions({
    queryKey: ["billing", "subscriptions"] as const,
    queryFn: () => getSubscriptions(),
  });

export const activeGrantsQuery = () =>
  queryOptions({
    queryKey: ["billing", "grants", { active: true }] as const,
    queryFn: () => getGrants({ data: { active: true } }),
  });

export async function loadBalance(queryClient: QueryClient) {
  return queryClient.ensureQueryData(balanceQuery());
}

export async function loadBillingPage(queryClient: QueryClient) {
  const [balance, subscriptions, grants] = await Promise.all([
    queryClient.ensureQueryData(balanceQuery()),
    queryClient.ensureQueryData(subscriptionsQuery()),
    queryClient.ensureQueryData(activeGrantsQuery()),
  ]);

  return {
    balance,
    subscriptions,
    grants,
  };
}
