import { Link, type ErrorComponentProps, type NotFoundRouteProps } from "@tanstack/react-router";
import { Skeleton } from "@forge-metal/ui";
import { Button } from "@forge-metal/ui/components/ui/button";
import { EmptyState } from "./empty-state";
import { ErrorCallout } from "./error-callout";

export function AppPending() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-6 w-40 rounded-none" />
        <Skeleton className="h-4 w-72 max-w-full rounded-none" />
      </div>
      <div className="grid gap-4">
        <Skeleton className="h-28 w-full rounded-none" />
        <Skeleton className="h-28 w-full rounded-none" />
      </div>
    </div>
  );
}

export function AppRouteError({ error, reset }: ErrorComponentProps) {
  return (
    <div className="space-y-6">
      <ErrorCallout
        title="Unable to load this page"
        error={error}
        action={
          <div className="flex flex-wrap gap-3">
            <Button type="button" variant="default" className="rounded-none" onClick={() => reset()}>
              Retry
            </Button>
            <Button asChild variant="outline" className="rounded-none">
              <Link to="/executions">Back to executions</Link>
            </Button>
          </div>
        }
      />
    </div>
  );
}

export function AppNotFound(_props: NotFoundRouteProps) {
  return (
    <EmptyState
      title="Not found"
      body="The page or resource you requested does not exist."
      action={
        <Button asChild variant="default" className="rounded-none">
          <Link to="/executions">Return to executions</Link>
        </Button>
      }
    />
  );
}
