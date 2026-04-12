import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  cancelSubscription,
  createCheckoutSession,
  createPortalSession,
  createSubscriptionSession,
} from "~/server-fns/api";
import { entitlementsQuery, subscriptionsQuery } from "./queries";

const sandboxProductID = "sandbox";

export function useCreateCheckoutSessionMutation() {
  return useMutation({
    mutationFn: (amountCents: number) =>
      createCheckoutSession({
        data: {
          product_id: sandboxProductID,
          amount_cents: amountCents,
          success_url: `${window.location.origin}/billing?purchased=true`,
          cancel_url: `${window.location.origin}/billing/credits`,
        },
      }),
    onSuccess: (data) => {
      window.location.assign(data.url);
    },
  });
}

export function useCreateSubscriptionSessionMutation() {
  return useMutation({
    mutationFn: (planId: string) =>
      createSubscriptionSession({
        data: {
          plan_id: planId,
          cadence: "monthly",
          success_url: `${window.location.origin}/billing?subscribed=true`,
          cancel_url: `${window.location.origin}/billing/subscribe`,
        },
      }),
    onSuccess: (data) => {
      window.location.assign(data.url);
    },
  });
}

export function useCreatePortalSessionMutation() {
  return useMutation({
    mutationFn: () =>
      createPortalSession({
        data: {
          return_url: `${window.location.origin}/billing`,
        },
      }),
    onSuccess: (data) => {
      window.location.assign(data.url);
    },
  });
}

export function useCancelSubscriptionMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (subscriptionId: string) =>
      cancelSubscription({
        data: {
          subscriptionId,
        },
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: entitlementsQuery(auth).queryKey }),
        queryClient.invalidateQueries({ queryKey: subscriptionsQuery(auth).queryKey }),
      ]);
    },
  });
}

export { useCreateCheckoutSessionMutation as useCreditCheckoutMutation };
export { useCreateSubscriptionSessionMutation as useSubscriptionCheckoutMutation };
export { useCreatePortalSessionMutation as useBillingPortalMutation };
export { useCancelSubscriptionMutation as useSubscriptionCancelMutation };
