import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import {
  cancelContract,
  createCheckoutSession,
  createContractChangeSession,
  createContractSession,
  createPortalSession,
} from "~/server-fns/api";
import { contractsQuery, entitlementsQuery } from "./queries";

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

export function useCreateContractSessionMutation() {
  return useMutation({
    mutationFn: ({ planId, action = "start" }: { planId: string; action?: string }) =>
      createContractSession({
        data: {
          plan_id: planId,
          cadence: "monthly",
          success_url: billingSuccessURL({ contractAction: action, targetPlanID: planId }),
          cancel_url: `${window.location.origin}/billing/subscribe`,
        },
      }),
    onSuccess: (data) => {
      window.location.assign(data.url);
    },
  });
}

export function useCreateContractChangeSessionMutation() {
  return useMutation({
    mutationFn: ({
      contractId,
      targetPlanId,
      action,
    }: {
      contractId: string;
      targetPlanId: string;
      action: "upgrade" | "downgrade" | "resume";
    }) =>
      createContractChangeSession({
        data: {
          contract_id: contractId,
          target_plan_id: targetPlanId,
          success_url: billingSuccessURL({
            contractAction: action,
            targetPlanID: targetPlanId,
          }),
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

export function useCancelContractMutation() {
  const auth = useSignedInAuth();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (contractId: string) =>
      cancelContract({
        data: {
          contractId,
        },
      }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: entitlementsQuery(auth).queryKey }),
        queryClient.invalidateQueries({ queryKey: contractsQuery(auth).queryKey }),
      ]);
    },
  });
}

export { useCreateCheckoutSessionMutation as useCreditCheckoutMutation };
export { useCreatePortalSessionMutation as useBillingPortalMutation };

function billingSuccessURL(params: Record<string, string>) {
  const search = new URLSearchParams({ contracted: "true", ...params });
  return `${window.location.origin}/billing?${search.toString()}`;
}
