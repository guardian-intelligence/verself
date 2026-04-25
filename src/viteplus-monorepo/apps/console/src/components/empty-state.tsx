import type { ReactNode } from "react";
import { cn } from "@verself/ui/lib/utils";

export function EmptyState({
  title,
  body,
  action,
  icon,
  className,
}: {
  title: string;
  body: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-md border border-dashed bg-card px-8 py-12 text-center",
        className,
      )}
    >
      {icon ? <div className="text-muted-foreground">{icon}</div> : null}
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="max-w-prose text-sm text-muted-foreground">{body}</div>
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}
