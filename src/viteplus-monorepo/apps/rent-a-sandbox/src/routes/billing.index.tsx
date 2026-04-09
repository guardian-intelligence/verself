import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { BalanceCard } from "~/components/balance-card";
import { Callout } from "~/components/callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import {
  activeGrantsQuery,
  balanceQuery,
  loadBillingPage,
  subscriptionsQuery,
} from "~/features/billing/queries";
import { SubscriptionStatusPill } from "~/features/billing/components";
import { requireViewer } from "~/lib/protected-route";

export const Route = createFileRoute("/billing/")({
  validateSearch: (search: Record<string, unknown>) => ({
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  }),
  beforeLoad: ({ location }) => requireViewer(location.href),
  loader: ({ context }) => loadBillingPage(context.queryClient),
  component: BillingPage,
});

function BillingPage() {
  const { purchased, subscribed } = Route.useSearch();
  const balance = useSuspenseQuery(balanceQuery()).data;
  const subscriptions = useSuspenseQuery(subscriptionsQuery()).data;
  const grants = useSuspenseQuery(activeGrantsQuery()).data;

  const subscriptionRows = subscriptions.subscriptions ?? [];
  const grantRows = grants.grants ?? [];

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Billing</h1>
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

      {purchased || subscribed ? (
        <Callout
          tone="success"
          title={purchased ? "Credits purchased" : "Subscription activated"}
        >
          {purchased
            ? "Credits purchased successfully. Your balance has been updated."
            : "Subscription activated. Monthly credits will be deposited automatically."}
        </Callout>
      ) : null}

      <BalanceCard balance={balance} />

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Subscriptions</h2>
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-2 text-left font-medium">Plan</th>
                <th className="px-4 py-2 text-left font-medium">Status</th>
                <th className="px-4 py-2 text-left font-medium">Cadence</th>
                <th className="px-4 py-2 text-left font-medium">Period End</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {subscriptionRows.length > 0 ? (
                subscriptionRows.map((subscription) => (
                  <tr key={subscription.subscription_id}>
                    <td className="px-4 py-2 font-medium">{subscription.plan_id}</td>
                    <td className="px-4 py-2">
                      <SubscriptionStatusPill status={subscription.status} />
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
                <TableEmptyRow colSpan={4}>No active subscriptions.</TableEmptyRow>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Active Credit Grants</h2>
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-2 text-left font-medium">Source</th>
                <th className="px-4 py-2 text-left font-medium">Amount</th>
                <th className="px-4 py-2 text-left font-medium">Product</th>
                <th className="px-4 py-2 text-left font-medium">Expires</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {grantRows.length > 0 ? (
                grantRows.map((grant) => (
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
                <TableEmptyRow colSpan={4}>No active credit grants.</TableEmptyRow>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
