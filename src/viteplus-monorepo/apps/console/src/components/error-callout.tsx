import type { ReactNode } from "react";
import { Alert, AlertDescription, AlertTitle } from "@forge-metal/ui/components/ui/alert";
import { cn } from "@forge-metal/ui/lib/utils";

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
    <Alert role="alert" variant="destructive" className={cn(className)}>
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{errorMessage(error, "An unexpected error occurred.")}</AlertDescription>
      {action ? <div className="mt-3">{action}</div> : null}
    </Alert>
  );
}
