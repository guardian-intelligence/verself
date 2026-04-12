import { Callout } from "~/components/callout";
import type { BillingFlashSearch } from "./search";

export function SubscriptionStatusPill({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: "bg-green-100 text-green-800",
    canceled: "bg-red-100 text-red-800",
    past_due: "bg-yellow-100 text-yellow-800",
  };

  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${colors[status] ?? "bg-muted text-muted-foreground"}`}
    >
      {status}
    </span>
  );
}

export function BillingFlashNotice({ purchased, subscribed }: BillingFlashSearch) {
  if (!purchased && !subscribed) {
    return null;
  }

  return (
    <Callout tone="success" title={purchased ? "Credits purchased" : "Subscription activated"}>
      {purchased
        ? "Credits purchased successfully. Your account credit pool has been updated."
        : "Subscription activated. Monthly bucket allowances will be deposited automatically."}
    </Callout>
  );
}
