import * as React from "react";
import { ChevronDown } from "lucide-react";

import { cn } from "@forge-metal/ui/lib/utils";

// Styled native <select>. We intentionally do not use a custom popover-based
// select — Base UI's Select is a fastComponent and trips the SSR module-graph
// bug tracked in app-shell.tsx. Native select is SSR-safe, keyboard-accessible
// for free, and sufficient for short option lists.

function Select({ className, children, ...props }: React.ComponentProps<"select">) {
  return (
    <div className="relative inline-flex w-full min-w-0">
      <select
        data-slot="select"
        className={cn(
          "h-9 w-full min-w-0 appearance-none rounded-md border border-input bg-input/20 pr-8 pl-3 py-1 text-sm transition-colors outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/30 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-2 aria-invalid:ring-destructive/20 dark:bg-input/30",
          className,
        )}
        {...props}
      >
        {children}
      </select>
      <ChevronDown
        aria-hidden="true"
        className="pointer-events-none absolute right-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
      />
    </div>
  );
}

export { Select };
