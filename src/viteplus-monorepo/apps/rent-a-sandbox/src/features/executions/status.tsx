import { Badge } from "@forge-metal/ui/components/ui/badge";

export function isExecutionActiveStatus(status?: string): boolean {
  return (
    status === "queued" ||
    status === "reserved" ||
    status === "launching" ||
    status === "running" ||
    status === "finalizing"
  );
}

const TERMINAL_SUCCESS = new Set(["succeeded"]);
const TERMINAL_FAILURE = new Set(["failed", "lost"]);
const TERMINAL_NEUTRAL = new Set(["canceled"]);

export function ExecutionStatusBadge({ status }: { status: string }) {
  let variant: "default" | "secondary" | "outline" | "destructive" = "secondary";
  if (TERMINAL_SUCCESS.has(status)) variant = "default";
  else if (TERMINAL_FAILURE.has(status)) variant = "destructive";
  else if (TERMINAL_NEUTRAL.has(status)) variant = "outline";

  return (
    <Badge variant={variant} data-execution-status={status}>
      {status}
    </Badge>
  );
}
