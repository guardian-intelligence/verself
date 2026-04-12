import { createFileRoute, Link } from "@tanstack/react-router";
import { useAuth } from "@forge-metal/auth-web/react";
import { EmptyState } from "~/components/empty-state";

export const Route = createFileRoute("/")({
  component: Dashboard,
});

function Dashboard() {
  const { isSignedIn } = useAuth();
  if (!isSignedIn) {
    return <GuestLanding />;
  }

  return <MemberDashboard />;
}

function GuestLanding() {
  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <h1 className="text-2xl font-bold">Rent-a-Sandbox</h1>
        <p className="text-sm text-muted-foreground">Firecracker sandboxes on bare metal.</p>
      </div>

      <EmptyState
        title="Sign in to manage sandboxes"
        body="View your entitlements, import repos, and run sandboxed executions from one place."
        action={
          <a
            href={`/login?redirect=${encodeURIComponent("/")}`}
            className="inline-flex rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90"
          >
            Sign in
          </a>
        }
      />

      <div className="grid gap-4 md:grid-cols-2">
        <Link
          to="/repos"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Repos</h3>
          <p className="text-sm text-muted-foreground">
            Import a repository and track clone metadata.
          </p>
        </Link>
        <Link
          to="/billing"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Billing</h3>
          <p className="text-sm text-muted-foreground">
            Manage subscriptions, credits, and entitlements.
          </p>
        </Link>
      </div>
    </div>
  );
}

function MemberDashboard() {
  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="flex gap-3">
          <Link
            to="/billing"
            className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
          >
            View Entitlements
          </Link>
          <Link
            to="/billing/credits"
            className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90"
          >
            Buy Credits
          </Link>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Link
          to="/repos"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Repos</h3>
          <p className="text-sm text-muted-foreground">
            Import a repository and track clone metadata.
          </p>
        </Link>
        <Link
          to="/jobs"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Executions</h3>
          <p className="text-sm text-muted-foreground">Monitor direct VM executions and logs.</p>
        </Link>
      </div>

      <Link
        to="/billing"
        className="block rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
      >
        <h3 className="mb-1 font-semibold">Billing</h3>
        <p className="text-sm text-muted-foreground">
          Manage subscriptions, credits, and entitlements.
        </p>
      </Link>
    </div>
  );
}
