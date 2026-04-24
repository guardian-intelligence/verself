import type { ComponentProps } from "react";
import { formatDateTimeUTC } from "~/lib/format";
import { cn } from "@forge-metal/ui/lib/utils";

export type AutoSyncState = "idle" | "syncing";

export function AutoSyncStatus({
  className,
  state,
  syncedAt,
  ...props
}: {
  readonly className?: string;
  readonly state: AutoSyncState;
  readonly syncedAt?: string | undefined;
} & Omit<ComponentProps<"p">, "children">) {
  const lastSynced = syncedAt ? `Last synced ${formatDateTimeUTC(syncedAt)}` : "Not synced yet";
  const text = state === "syncing" ? `Syncing changes - ${lastSynced}` : lastSynced;

  return (
    <p
      aria-live="polite"
      className={cn("text-xs font-medium text-muted-foreground", className)}
      {...props}
      data-sync-state={state}
      data-synced-at={syncedAt ?? ""}
    >
      {text}
    </p>
  );
}
