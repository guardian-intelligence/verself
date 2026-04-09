import type { ReactNode } from "react";
import { Callout } from "~/components/callout";
import { TableEmptyRow as AppTableEmptyRow } from "~/components/table-empty-row";

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

export function TableEmptyRow({
  colSpan,
  children,
}: {
  colSpan: number;
  children: ReactNode;
}) {
  return <AppTableEmptyRow colSpan={colSpan}>{children}</AppTableEmptyRow>;
}

export function BillingBanner({ children }: { children: ReactNode }) {
  return (
    <Callout tone="success">
      {children}
    </Callout>
  );
}
