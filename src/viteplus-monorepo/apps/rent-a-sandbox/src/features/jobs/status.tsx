const EXECUTION_STATUS_CLASSES: Record<string, string> = {
  queued: "bg-yellow-100 text-yellow-800",
  reserved: "bg-amber-100 text-amber-800",
  launching: "bg-sky-100 text-sky-800",
  running: "bg-blue-100 text-blue-800",
  finalizing: "bg-indigo-100 text-indigo-800",
  succeeded: "bg-green-100 text-green-800",
  failed: "bg-red-100 text-red-800",
  canceled: "bg-zinc-200 text-zinc-800",
  lost: "bg-fuchsia-100 text-fuchsia-800",
};

export function isExecutionActiveStatus(status?: string): boolean {
  return (
    status === "queued" ||
    status === "reserved" ||
    status === "launching" ||
    status === "running" ||
    status === "finalizing"
  );
}

export function executionStatusClass(status: string): string {
  return EXECUTION_STATUS_CLASSES[status] ?? "bg-muted text-muted-foreground";
}

export function ExecutionStatusBadge({ status }: { status: string }) {
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${executionStatusClass(status)}`}
    >
      {status}
    </span>
  );
}
