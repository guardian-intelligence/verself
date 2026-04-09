import { createFileRoute, Link } from "@tanstack/react-router";
import { BalanceCard } from "~/components/balance-card";
import { getBalance } from "~/server-fns/api";
import { getViewer } from "~/server-fns/auth";

export const Route = createFileRoute("/")({
  validateSearch: (search: Record<string, unknown>) => ({
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  }),
  loader: async () => {
    const viewer = await getViewer();
    if (!viewer) {
      return {
        viewer: null,
        balance: null,
      };
    }
    return {
      viewer,
      balance: await getBalance(),
    };
  },
  component: Dashboard,
});

function Dashboard() {
  const { purchased, subscribed } = Route.useSearch();
  const { viewer, balance } = Route.useLoaderData();

  if (!viewer) {
    return (
      <div className="space-y-8">
        <h1 className="text-2xl font-bold">Rent-a-Sandbox</h1>
        <div className="border border-border rounded-lg p-8 text-center space-y-3">
          <p className="text-lg text-muted-foreground">Firecracker CI sandboxes on bare metal.</p>
          <p className="text-muted-foreground">
            Sign in to view your credit balance and run sandboxes.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <div className="flex gap-3">
          <Link
            to="/billing/subscribe"
            className="px-4 py-2 rounded-md border border-border hover:bg-accent text-sm"
          >
            Subscribe
          </Link>
          <Link
            to="/billing/credits"
            className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm"
          >
            Buy Credits
          </Link>
        </div>
      </div>

      {(purchased || subscribed) && (
        <div className="border border-success/50 bg-success/5 rounded-lg p-4 text-sm">
          {purchased
            ? "Credits purchased successfully! Your balance has been updated."
            : "Subscription activated! Monthly credits will be deposited automatically."}
        </div>
      )}

      {balance ? (
        <>
          <BalanceCard balance={balance} />
          {balance.total_available <= 0 && (
            <div className="border border-destructive/50 bg-destructive/5 rounded-lg p-4 text-sm flex items-center justify-between">
              <span>Your credit balance is empty. Purchase credits to create sandboxes.</span>
              <Link
                to="/billing/credits"
                className="px-3 py-1.5 rounded-md bg-primary text-primary-foreground hover:opacity-90 text-sm whitespace-nowrap"
              >
                Buy Credits
              </Link>
            </div>
          )}
        </>
      ) : null}

      <div className="grid md:grid-cols-2 gap-4">
        <Link
          to="/repos"
          className="border border-border rounded-lg p-6 hover:bg-accent/50 transition-colors"
        >
          <h3 className="font-semibold mb-1">Repos</h3>
          <p className="text-sm text-muted-foreground">
            Import a repository, prepare its golden image, and track readiness
          </p>
        </Link>
        <Link
          to="/jobs"
          className="border border-border rounded-lg p-6 hover:bg-accent/50 transition-colors"
        >
          <h3 className="font-semibold mb-1">Executions</h3>
          <p className="text-sm text-muted-foreground">
            Monitor repo executions, golden builds, and runner attempts
          </p>
        </Link>
      </div>

      <div className="grid md:grid-cols-1 gap-4">
        <Link
          to="/billing"
          search={{ purchased: false, subscribed: false }}
          className="border border-border rounded-lg p-6 hover:bg-accent/50 transition-colors"
        >
          <h3 className="font-semibold mb-1">Billing</h3>
          <p className="text-sm text-muted-foreground">Manage subscriptions, credits, and grants</p>
        </Link>
      </div>
    </div>
  );
}
