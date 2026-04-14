import type { ReactNode } from "react";
import { Alert, AlertDescription, AlertTitle } from "@forge-metal/ui/components/ui/alert";
import { cn } from "@forge-metal/ui/lib/utils";

type CalloutTone = "default" | "success" | "warning" | "destructive";

const toneToAlertVariant: Record<CalloutTone, "default" | "destructive"> = {
  default: "default",
  success: "default",
  warning: "default",
  destructive: "destructive",
};

// Black-and-white receipt variant: tone is conveyed by the left border
// weight and the `data-callout-tone` attribute, not background color.
const toneBorderClass: Record<CalloutTone, string> = {
  default: "border-l-2 border-l-foreground",
  success: "border-l-2 border-l-foreground",
  warning: "border-l-2 border-l-foreground border-dashed",
  destructive: "border-l-2 border-l-foreground",
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
      variant={toneToAlertVariant[tone]}
      className={cn(
        "flex items-start justify-between gap-4 rounded-none border-foreground",
        toneBorderClass[tone],
        className,
      )}
    >
      <div className="min-w-0 flex-1 space-y-1">
        {title ? (
          <AlertTitle className="font-mono text-xs uppercase tracking-wider">{title}</AlertTitle>
        ) : null}
        <AlertDescription className="text-sm">{children}</AlertDescription>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </Alert>
  );
}
