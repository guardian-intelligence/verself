import { Badge } from "@forge-metal/ui/components/ui/badge";
import { Callout } from "~/components/callout";
import { formatDateTimeUTC } from "~/lib/format";
import type { FlashIntent } from "./flash";
import { assertUnreachable } from "./state";

// Receipt design: status is conveyed by a single monochrome badge shape
// plus a `data-contract-status` hook. e2e selectors can target the data
// attribute; visual distinction comes from variant, not color.
const TERMINAL_SUCCESS = new Set(["active"]);
const TERMINAL_NEUTRAL = new Set(["cancel_scheduled", "suspended"]);
const TERMINAL_FAILURE = new Set(["ended", "voided"]);

export function ContractStatusPill({ status }: { status: string }) {
  let variant: "default" | "secondary" | "outline" | "destructive" = "outline";
  if (TERMINAL_SUCCESS.has(status)) variant = "default";
  else if (TERMINAL_FAILURE.has(status)) variant = "destructive";
  else if (TERMINAL_NEUTRAL.has(status)) variant = "secondary";

  return (
    <Badge
      variant={variant}
      data-contract-status={status}
      className="rounded-none border-foreground font-mono text-[10px] uppercase tracking-wider"
    >
      {status}
    </Badge>
  );
}

// Renders a tonally-correct banner for every post-Stripe redirect. The DU
// wiring forces every new FlashIntent kind to be handled here or the
// TypeScript exhaustive switch fails at compile time.
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
