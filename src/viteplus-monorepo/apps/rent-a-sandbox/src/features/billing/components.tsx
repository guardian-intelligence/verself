import { Callout } from "~/components/callout";
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

export function BillingFlashNotice({ purchased, contracted }: BillingFlashSearch) {
  if (!purchased && !contracted) {
    return null;
  }

  return (
    <Callout tone="success" title={purchased ? "Credits purchased" : "Contract activated"}>
      {purchased
        ? "Credits purchased successfully. Your account credit pool has been updated."
        : "Contract activated. Monthly bucket allowances will be deposited automatically."}
    </Callout>
  );
}
