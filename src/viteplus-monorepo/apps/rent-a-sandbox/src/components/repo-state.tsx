export function RepoStateBadge({ state }: { state: string }) {
  const colors: Record<string, string> = {
    importing: "bg-slate-100 text-slate-800",
    action_required: "bg-amber-100 text-amber-900",
    waiting_for_bootstrap: "bg-yellow-100 text-yellow-900",
    preparing: "bg-sky-100 text-sky-900",
    ready: "bg-green-100 text-green-900",
    degraded: "bg-orange-100 text-orange-900",
    failed: "bg-red-100 text-red-900",
    archived: "bg-zinc-200 text-zinc-800",
  };
  return (
    <span
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[state] ?? "bg-muted text-muted-foreground"}`}
    >
      {state.replaceAll("_", " ")}
    </span>
  );
}

export function shortSHA(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 12);
}

export function shortID(value?: string): string {
  if (!value) return "--";
  return value.slice(0, 8);
}
