import { Callout } from "~/components/callout";
import { formatDateTimeUTC } from "~/lib/format";
import type { BillingFlashSearch } from "./search";

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

export function BillingFlashNotice({
  purchased,
  contracted,
  contractAction,
  contractEffectiveAt,
}: BillingFlashSearch) {
  if (!purchased && !contracted) {
    return null;
  }

  if (contracted) {
    const message = contractFlashMessage(contractAction, contractEffectiveAt);
    return (
      <Callout tone="success" title={message.title}>
        {message.body}
      </Callout>
    );
  }

  return (
    <Callout tone="success" title="Credits purchased">
      Credits purchased successfully. Your account credit pool has been updated.
    </Callout>
  );
}

function contractFlashMessage(action: BillingFlashSearch["contractAction"], effectiveAt?: string) {
  const effectiveText = formatEffectiveAt(effectiveAt);
  switch (action) {
    case "upgrade":
      return {
        title: "Plan upgrade complete",
        body: "Your upgraded plan is active now and the prorated allowance delta has been applied.",
      };
    case "downgrade":
      return {
        title: "Plan downgrade scheduled",
        body: effectiveText
          ? `Your current plan stays active until the next billing finalization at ${effectiveText}.`
          : "Your current plan stays active until the next billing finalization.",
      };
    case "resume":
      return {
        title: "Plan resumed",
        body: "The scheduled downgrade or cancellation was canceled. Your current plan remains active.",
      };
    case "unchanged":
      return {
        title: "Plan unchanged",
        body: "You are already on this plan, so billing did not start a checkout session.",
      };
    case "start":
    default:
      return {
        title: "Plan checkout complete",
        body: "Stripe accepted the checkout. Your monthly allowances will be deposited after billing applies the provider event.",
      };
  }
}

function formatEffectiveAt(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return formatDateTimeUTC(date);
}
