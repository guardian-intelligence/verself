import { Link, type ErrorComponentProps, type NotFoundRouteProps } from "@tanstack/react-router";
import { Button } from "@verself/ui/components/ui/button";
import { Skeleton } from "@verself/ui/components/ui/skeleton";
import { resolveShellFallbackPath } from "~/features/shell/fallback-routing";
import { useOptionalOrgSlug } from "~/features/shell/org-routing";
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
  const fallbackPath = useShellFallbackPath();
  return (
    <div className="space-y-6">
      <ErrorCallout
        title="Unable to load this page"
        error={error}
        action={
          <div className="flex flex-wrap gap-3">
            <Button type="button" onClick={() => reset()}>
              Retry
            </Button>
            <Button
              variant="outline"
              render={<Link from="/" to={fallbackPath} search={() => ({})} />}
            >
              Back to console
            </Button>
          </div>
        }
      />
    </div>
  );
}

export function AppNotFound(props: NotFoundRouteProps) {
  const fallbackPath = useShellFallbackPath(props.data);
  return (
    <EmptyState
      title="Not found"
      body="The page or resource you requested does not exist."
      action={
        <Button render={<Link from="/" to={fallbackPath} search={() => ({})} />}>
          Return to console
        </Button>
      }
    />
  );
}

function useShellFallbackPath(notFoundData?: unknown): string {
  const orgSlug = useOptionalOrgSlug();
  return resolveShellFallbackPath({ notFoundData, orgSlug });
}
