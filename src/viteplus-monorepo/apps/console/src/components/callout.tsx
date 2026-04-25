import type { ReactNode } from "react";
import { Alert, AlertDescription, AlertTitle } from "@verself/ui/components/ui/alert";
import { cn } from "@verself/ui/lib/utils";

type CalloutTone = "default" | "success" | "warning" | "destructive";

const toneClasses: Record<CalloutTone, string> = {
  default: "bg-muted/30",
  success: "border-emerald-500/50 bg-emerald-500/5",
  warning: "border-amber-500/50 bg-amber-500/5",
  destructive: "border-destructive/50 bg-destructive/5",
};

export function Callout({
  title,
  children,
  tone = "default",
  action,
  className,
}: {
  title?: string;
  children: ReactNode;
  tone?: CalloutTone;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <Alert
      data-callout-tone={tone}
      variant={tone === "destructive" ? "destructive" : "default"}
      className={cn("flex items-start justify-between gap-4", toneClasses[tone], className)}
    >
      <div className="min-w-0 flex-1 space-y-1">
        {title ? <AlertTitle>{title}</AlertTitle> : null}
        <AlertDescription className="text-sm">{children}</AlertDescription>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </Alert>
  );
}
