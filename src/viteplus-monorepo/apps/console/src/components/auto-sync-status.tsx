import type { ComponentProps } from "react";
import { ClientOnly } from "@tanstack/react-router";
import { ElapsedTime } from "@forge-metal/ui/components/elapsed-time";
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
  const lastSynced = syncedAt ? (
    <>
      Last synced{" "}
      <ClientOnly fallback={null}>
        <ElapsedTime dateTime={syncedAt} title={formatDateTimeUTC(syncedAt)} value={syncedAt} />
      </ClientOnly>
    </>
  ) : (
    "Not synced yet"
  );

  return (
    <p
      aria-live={state === "syncing" ? "polite" : undefined}
      className={cn("text-xs font-medium text-muted-foreground", className)}
      {...props}
      data-sync-state={state}
      data-synced-at={syncedAt ?? ""}
    >
      {state === "syncing" ? <>Syncing changes - {lastSynced}</> : lastSynced}
    </p>
  );
}
