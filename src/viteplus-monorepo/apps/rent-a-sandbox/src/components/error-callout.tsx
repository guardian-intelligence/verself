import type { ReactNode } from "react";
import { cn } from "@forge-metal/ui";

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return fallback;
}

export function ErrorCallout({
  title = "Something went wrong",
  error,
  action,
  className,
}: {
  title?: string;
  error: unknown;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm text-destructive",
        className,
      )}
      role="alert"
    >
      <div className="font-medium">{title}</div>
      <p className="mt-1">{errorMessage(error, "An unexpected error occurred.")}</p>
      {action ? <div className="mt-3">{action}</div> : null}
    </div>
  );
}
