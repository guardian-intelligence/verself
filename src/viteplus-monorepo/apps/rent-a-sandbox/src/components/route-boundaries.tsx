import { Link, type ErrorComponentProps, type NotFoundRouteProps } from "@tanstack/react-router";
import { Skeleton } from "@forge-metal/ui";
import { EmptyState } from "./empty-state";
import { ErrorCallout } from "./error-callout";

export function AppPending() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-80 max-w-full" />
      </div>
      <div className="grid gap-4">
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-28 w-full" />
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
          <div className="flex gap-3">
            <button
              type="button"
              onClick={() => reset()}
              className="rounded-md bg-primary px-3 py-1.5 text-primary-foreground hover:opacity-90"
            >
              Retry
            </button>
            <Link
              to="/"
              search={{ purchased: false, subscribed: false }}
              className="rounded-md border border-border px-3 py-1.5 text-foreground hover:bg-accent"
            >
              Back to dashboard
            </Link>
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
        <Link
          to="/"
          search={{ purchased: false, subscribed: false }}
          className="inline-flex rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90"
        >
          Return to dashboard
        </Link>
      }
    />
  );
}
