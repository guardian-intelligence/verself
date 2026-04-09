import type { ReactNode } from "react";
import { cn } from "@forge-metal/ui";

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
      className={cn("rounded-lg border border-border bg-card px-8 py-10 text-center", className)}
    >
      {icon ? (
        <div className="mx-auto mb-4 flex justify-center text-muted-foreground">{icon}</div>
      ) : null}
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="mt-2 text-sm text-muted-foreground">{body}</div>
      {action ? <div className="mt-5">{action}</div> : null}
    </div>
  );
}
