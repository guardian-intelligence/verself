import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useSignedInAuth } from "@forge-metal/auth-web/react";
import { BalanceCard } from "~/components/balance-card";
import { ErrorCallout } from "~/components/error-callout";
import { TableEmptyRow } from "~/components/table-empty-row";
import { BillingFlashNotice, SubscriptionStatusPill } from "~/features/billing/components";
import { useCreatePortalSessionMutation } from "~/features/billing/mutations";
import {
  activeGrantsQuery,
  balanceQuery,
  loadBillingPage,
  subscriptionsQuery,
} from "~/features/billing/queries";
import { parseBillingFlashSearch } from "~/features/billing/search";
import { formatDateUTC, formatInteger } from "~/lib/format";

export const Route = createFileRoute("/_authenticated/billing/")({
  validateSearch: parseBillingFlashSearch,
  loader: ({ context }) => loadBillingPage(context.queryClient, context.auth),
  component: BillingPage,
});

function BillingPage() {
  const auth = useSignedInAuth();
  const flash = Route.useSearch();
  const balance = useSuspenseQuery(balanceQuery(auth)).data;
  const subscriptions = useSuspenseQuery(subscriptionsQuery(auth)).data;
  const grants = useSuspenseQuery(activeGrantsQuery(auth)).data;
  const portalMutation = useCreatePortalSessionMutation();

  const subscriptionRows = subscriptions.subscriptions ?? [];
  const grantRows = grants.grants ?? [];

  return (
    <div className="space-y-8">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold">Billing</h1>
        <div className="flex flex-wrap gap-3">
          {subscriptionRows.length > 0 ? (
            <button
              type="button"
              onClick={() => portalMutation.mutate()}
              disabled={portalMutation.isPending}
              className="px-4 py-2 min-w-[128px] rounded-md border border-border hover:bg-accent text-sm disabled:opacity-50"
            >
              {portalMutation.isPending ? "Opening..." : "Manage Billing"}
            </button>
          ) : null}
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

      <BillingFlashNotice {...flash} />

      {portalMutation.error ? (
        <ErrorCallout error={portalMutation.error} title="Billing portal failed" />
      ) : null}

      <BalanceCard balance={balance} />

      <section className="space-y-3">
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
                        ? formatDateUTC(subscription.current_period_end)
                        : "--"}
                    </td>
                  </tr>
                ))
              ) : (
                <TableEmptyRow
                  colSpan={4}
                  title="No active subscriptions"
                  description="Subscribe to start receiving bucketed usage credits."
                />
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-semibold mb-3">Active Credit Grants</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Source</th>
                <th className="text-left px-4 py-2 font-medium">Scope</th>
                <th className="text-left px-4 py-2 font-medium">Available</th>
                <th className="text-left px-4 py-2 font-medium">Pending</th>
                <th className="text-left px-4 py-2 font-medium">Expires</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {grantRows.length > 0 ? (
                grantRows.map((grant) => (
                  <tr key={grant.grant_id}>
                    <td className="px-4 py-2">{grant.source}</td>
                    <td className="px-4 py-2">{formatGrantScope(grant)}</td>
                    <td className="px-4 py-2 font-mono">{formatInteger(grant.available)}</td>
                    <td className="px-4 py-2 font-mono">{formatInteger(grant.pending)}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {grant.expires_at ? formatDateUTC(grant.expires_at) : "Never"}
                    </td>
                  </tr>
                ))
              ) : (
                <TableEmptyRow
                  colSpan={5}
                  title="No active credit grants"
                  description="Purchased or promotional grants will appear here."
                />
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function formatGrantScope(grant: {
  scope_type: string;
  scope_product_id: string;
  scope_bucket_id: string;
}) {
  switch (grant.scope_type) {
    case "account":
      return "Account";
    case "product":
      return `${grant.scope_product_id} / all buckets`;
    case "bucket":
      return `${grant.scope_product_id} / ${grant.scope_bucket_id}`;
    default:
      return grant.scope_type;
  }
}
