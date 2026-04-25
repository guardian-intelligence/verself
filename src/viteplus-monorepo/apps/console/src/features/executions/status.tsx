import { Badge } from "@verself/ui/components/ui/badge";

export function isExecutionActiveStatus(status?: string): boolean {
  return (
    status === "queued" ||
    status === "reserved" ||
    status === "launching" ||
    status === "running" ||
    status === "finalizing"
  );
}

// Five canonical status treatments. Every wire status collapses into one of
// these — transient states share a single visual so the list doesn't turn
// into a colour chart. If we ever need to distinguish (say) launching from
// running, add a subordinate signal like a dot or label, not a new pill.
type StatusKind = "queued" | "running" | "succeeded" | "failed" | "canceled";

const STATUS_KIND: Record<string, StatusKind> = {
  queued: "queued",
  reserved: "queued",
  launching: "running",
  running: "running",
  finalizing: "running",
  succeeded: "succeeded",
  failed: "failed",
  lost: "failed",
  canceled: "canceled",
};

const KIND_VARIANT: Record<
  StatusKind,
  "secondary" | "info" | "success" | "destructive" | "outline"
> = {
  queued: "secondary",
  running: "info",
  succeeded: "success",
  failed: "destructive",
  canceled: "outline",
};

export function ExecutionStatusBadge({ status }: { status?: string | null }) {
  const label = status?.trim() || "unknown";
  const kind = STATUS_KIND[label] ?? "queued";
  const variant = KIND_VARIANT[kind];

  return (
    <Badge variant={variant} data-execution-status={label} data-execution-status-kind={kind}>
      {kind === "running" ? (
        <span
          aria-hidden="true"
          className="size-1.5 rounded-full bg-current motion-safe:animate-pulse"
        />
      ) : null}
      {label}
    </Badge>
  );
}
