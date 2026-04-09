import { queryOptions } from "@tanstack/react-query";
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
