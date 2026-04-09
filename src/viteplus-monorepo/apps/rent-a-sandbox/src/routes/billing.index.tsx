import { createFileRoute, Link } from "@tanstack/react-router";
import { BalanceCard } from "~/components/balance-card";
import { getBalance, getGrants, getSubscriptions } from "~/server-fns/api";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/billing/")({
  validateSearch: (search: Record<string, unknown>) => ({
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  }),
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: async () => ({
    balance: await getBalance(),
    subscriptions: await getSubscriptions(),
    grants: await getGrants({ data: { active: true } }),
  }),
  component: BillingPage,
});

function BillingPage() {
  const { purchased, subscribed } = Route.useSearch();
  const {
    balance,
    subscriptions,
    grants,
  } = Route.useLoaderData();

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Billing</h1>
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

      <BalanceCard balance={balance} />

      <div>
        <h2 className="text-lg font-semibold mb-3">Subscriptions</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Plan</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-left px-4 py-2 font-medium">Cadence</th>
                <th className="text-left px-4 py-2 font-medium">Period End</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {subscriptions.subscriptions?.length ? (
                subscriptions.subscriptions.map((subscription) => (
                  <tr key={subscription.subscription_id}>
                    <td className="px-4 py-2 font-medium">{subscription.plan_id}</td>
                    <td className="px-4 py-2">
                      <StatusPill status={subscription.status} />
                    </td>
                    <td className="px-4 py-2">{subscription.cadence}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {subscription.current_period_end
                        ? new Date(subscription.current_period_end).toLocaleDateString()
                        : "--"}
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-muted-foreground">
                    No active subscriptions.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div>
        <h2 className="text-lg font-semibold mb-3">Active Credit Grants</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Source</th>
                <th className="text-left px-4 py-2 font-medium">Amount</th>
                <th className="text-left px-4 py-2 font-medium">Product</th>
                <th className="text-left px-4 py-2 font-medium">Expires</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {grants.grants?.length ? (
                grants.grants.map((grant) => (
                  <tr key={grant.grant_id}>
                    <td className="px-4 py-2">{grant.source}</td>
                    <td className="px-4 py-2 font-mono">{grant.amount.toLocaleString()}</td>
                    <td className="px-4 py-2">{grant.product_id}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {grant.expires_at ? new Date(grant.expires_at).toLocaleDateString() : "Never"}
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-muted-foreground">
                    No active credit grants.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function StatusPill({ status }: { status: string }) {
  const colors: Record<string, string> = {
    active: "bg-green-100 text-green-800",
    canceled: "bg-red-100 text-red-800",
    past_due: "bg-yellow-100 text-yellow-800",
  };
  return (
    <span
      className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[status] ?? "bg-muted text-muted-foreground"}`}
    >
      {status}
    </span>
  );
}
