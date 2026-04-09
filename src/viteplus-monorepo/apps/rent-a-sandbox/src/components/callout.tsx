import type { ReactNode } from "react";
import { cn } from "@forge-metal/ui";

const toneClasses = {
  default: "border-border bg-muted/20 text-foreground",
  success: "border-success/50 bg-success/5 text-foreground",
  warning: "border-warning/50 bg-warning/5 text-foreground",
  destructive: "border-destructive/50 bg-destructive/5 text-foreground",
} as const;

export function Callout({
  title,
  children,
  tone = "default",
  action,
  className,
}: {
  title?: string;
  children: ReactNode;
  tone?: keyof typeof toneClasses;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex items-start justify-between gap-4 rounded-lg border p-4 text-sm",
        toneClasses[tone],
        className,
      )}
    >
      <div className="min-w-0 space-y-1">
        {title ? <p className="font-medium">{title}</p> : null}
        <div className="text-muted-foreground">{children}</div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}
