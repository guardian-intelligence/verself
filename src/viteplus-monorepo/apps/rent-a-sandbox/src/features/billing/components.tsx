import { Callout } from "~/components/callout";
import { formatDateTimeUTC } from "~/lib/format";
import type { FlashIntent } from "./flash";
import { assertUnreachable } from "./state";

export function ContractStatusPill({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: "bg-green-100 text-green-800",
    cancel_scheduled: "bg-yellow-100 text-yellow-800",
    suspended: "bg-yellow-100 text-yellow-800",
    ended: "bg-red-100 text-red-800",
    voided: "bg-red-100 text-red-800",
  };

  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${colors[status] ?? "bg-muted text-muted-foreground"}`}
    >
      {status}
    </span>
  );
}

// Renders a tonally-correct banner for every post-Stripe redirect. The DU
// wiring forces every new FlashIntent kind to be handled here or the TypeScript
// exhaustive switch fails at compile time.
export function BillingFlashNotice({ intent }: { intent: FlashIntent }) {
  switch (intent.kind) {
    case "none":
      return null;
    case "credits_purchased":
      return (
        <Callout tone="success" title="Credits purchased">
          Credits purchased successfully. Your account credit pool has been updated.
        </Callout>
      );
    case "contract_started":
      return (
        <Callout tone="success" title="Plan checkout complete">
          Stripe accepted the checkout. Your monthly allowances will be deposited after billing
          applies the provider event.
        </Callout>
      );
    case "contract_upgraded":
      return (
        <Callout tone="success" title="Plan upgrade complete">
          Your upgraded plan is active now and the prorated allowance delta has been applied.
        </Callout>
      );
    case "contract_downgrade_scheduled":
      return (
        <Callout tone="success" title="Plan downgrade scheduled">
          {intent.effectiveAt
            ? `Your current plan stays active until the next billing finalization at ${formatDateTimeUTC(intent.effectiveAt)}.`
            : "Your current plan stays active until the next billing finalization."}
        </Callout>
      );
    case "contract_resumed":
      return (
        <Callout tone="success" title="Plan resumed">
          The scheduled downgrade or cancellation was canceled. Your current plan remains active.
        </Callout>
      );
    case "contract_unchanged":
      return (
        <Callout tone="success" title="Plan unchanged">
          You are already on this plan, so billing did not start a checkout session.
        </Callout>
      );
    default:
      return assertUnreachable(intent);
  }
}
