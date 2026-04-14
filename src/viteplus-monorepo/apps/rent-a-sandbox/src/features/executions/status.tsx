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

// Receipt design: single monochrome badge variant for every state. The
// *shape* of the word is the signal — bold uppercase for terminal,
// dimmer for pending — not color. Keeps the black/white design language
// consistent with the rest of the shell.
const TERMINAL_SUCCESS = new Set(["succeeded"]);
const TERMINAL_FAILURE = new Set(["failed", "lost"]);
const TERMINAL_NEUTRAL = new Set(["canceled"]);

export function ExecutionStatusBadge({ status }: { status: string }) {
  let variant: "default" | "secondary" | "outline" | "destructive" = "outline";
  if (TERMINAL_SUCCESS.has(status)) variant = "default";
  else if (TERMINAL_FAILURE.has(status)) variant = "destructive";
  else if (TERMINAL_NEUTRAL.has(status)) variant = "secondary";

  return (
    <Badge
      variant={variant}
      data-execution-status={status}
      className="rounded-none border-foreground font-mono text-[10px] uppercase tracking-wider"
    >
      {status}
    </Badge>
  );
}
