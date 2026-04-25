import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useSignedInAuth } from "@verself/auth-web/react";
import {
  cancelContract,
  createCheckoutSession,
  createContractChangeSession,
  createContractSession,
  createPortalSession,
} from "~/server-fns/api";
import type { BillingPlan } from "~/lib/sandbox-rental-api";
import { contractsQuery, entitlementsQuery } from "./queries";
import type { PlanCardIntent } from "./state";

const SANDBOX_PRODUCT_ID = "sandbox";

export function useCreateCheckoutSessionMutation() {
  return useMutation({
    mutationFn: (amountCents: number) =>
      createCheckoutSession({
        data: {
          product_id: SANDBOX_PRODUCT_ID,
          amount_cents: amountCents,
          success_url: `${window.location.origin}/settings/billing?purchased=true`,
          cancel_url: `${window.location.origin}/settings/billing/credits`,
        },
      }),
    onSuccess: (data) => {
      window.location.assign(data.url);
    },
  });
}

// Drives every plan-card click off a single PlanCardIntent discriminated
// union. `disabled` intents must never reach here — subscribe.tsx disables
// the button and PlanCardButton never fires onClick — but we still throw
// loudly if it slips through so a regression shows up as a crash rather
// than a silent no-op.
export function usePlanCardActionMutation() {
  return useMutation({
    mutationFn: async ({ intent, plan }: { intent: PlanCardIntent; plan: BillingPlan }) => {
      const baseSuccessURL = `${window.location.origin}/settings/billing?contracted=true`;
      const cancelURL = `${window.location.origin}/settings/billing/subscribe`;

      switch (intent.kind) {
        case "start":
          return await createContractSession({
            data: {
              plan_id: intent.planId,
              cadence: "monthly",
              success_url: baseSuccessURL,
              cancel_url: cancelURL,
            },
          });
        case "upgrade":
        case "downgrade":
        case "resume":
          return await createContractChangeSession({
            data: {
              contract_id: intent.contractId,
              target_plan_id: intent.targetPlanId,
              success_url: baseSuccessURL,
              cancel_url: cancelURL,
            },
          });
        case "disabled":
          throw new Error(
            `usePlanCardActionMutation: disabled intent clicked — plan=${plan.plan_id}`,
          );
      }
    },
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
          return_url: `${window.location.origin}/settings/billing`,
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
