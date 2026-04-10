import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import type { AuthenticatedAuthState } from "@forge-metal/auth-web";
import { BalanceCard } from "~/components/balance-card";
import { Callout } from "~/components/callout";
import { EmptyState } from "~/components/empty-state";
import { balanceQuery, loadBalance } from "~/features/billing/queries";

export const Route = createFileRoute("/")({
  loader: async ({ context }) => {
    if (context.authState.isAuthenticated) {
      await loadBalance(context.queryClient, context.authState);
    }
  },
  component: Dashboard,
});

function Dashboard() {
  const authState = Route.useRouteContext({ select: (context) => context.authState });
  if (!authState.isAuthenticated) {
    return <GuestLanding />;
  }

  return <MemberDashboard authState={authState} />;
}

function GuestLanding() {
  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <h1 className="text-2xl font-bold">Rent-a-Sandbox</h1>
        <p className="text-sm text-muted-foreground">Firecracker CI sandboxes on bare metal.</p>
      </div>

      <EmptyState
        title="Sign in to manage sandboxes"
        body="View your credit balance, import repos, and run sandboxed executions from one place."
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
            Import a repository, prepare its golden image, and track readiness.
          </p>
        </Link>
        <Link
          to="/billing"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Billing</h3>
          <p className="text-sm text-muted-foreground">
            Manage subscriptions, credits, and grants.
          </p>
        </Link>
      </div>
    </div>
  );
}

function MemberDashboard({ authState }: { authState: AuthenticatedAuthState }) {
  const { data: balance } = useSuspenseQuery(balanceQuery(authState));

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="flex gap-3">
          <Link
            to="/billing/subscribe"
            className="rounded-md border border-border px-4 py-2 text-sm hover:bg-accent"
          >
            Subscribe
          </Link>
          <Link
            to="/billing/credits"
            className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:opacity-90"
          >
            Buy Credits
          </Link>
        </div>
      </div>

      <BalanceCard balance={balance} />

      {balance.total_available <= 0 ? (
        <Callout
          tone="warning"
          title="Credit balance is empty"
          action={
            <Link
              to="/billing/credits"
              className="inline-flex rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:opacity-90"
            >
              Buy Credits
            </Link>
          }
        >
          Purchase credits to create sandboxes.
        </Callout>
      ) : null}

      <div className="grid gap-4 md:grid-cols-2">
        <Link
          to="/repos"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Repos</h3>
          <p className="text-sm text-muted-foreground">
            Import a repository, prepare its golden image, and track readiness.
          </p>
        </Link>
        <Link
          to="/jobs"
          className="rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="mb-1 font-semibold">Executions</h3>
          <p className="text-sm text-muted-foreground">
            Monitor repo executions, golden builds, and runner attempts.
          </p>
        </Link>
      </div>

      <Link
        to="/billing"
        className="block rounded-lg border border-border p-6 transition-colors hover:bg-accent/50"
      >
        <h3 className="mb-1 font-semibold">Billing</h3>
        <p className="text-sm text-muted-foreground">Manage subscriptions, credits, and grants.</p>
      </Link>
    </div>
  );
}
