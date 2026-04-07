import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { fetchBalance, fetchSubscriptions, fetchGrants } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { BalanceCard } from "~/components/balance-card";
import { Skeleton } from "~/components/ui/skeleton";

export const Route = createFileRoute("/billing/")({
  component: BillingPage,
  validateSearch: (search: Record<string, unknown>) => ({
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  }),
  beforeLoad: ({ search, context }) => {
    if (search.purchased || search.subscribed) {
      context.queryClient.invalidateQueries({ queryKey: keys.balance() });
      context.queryClient.invalidateQueries({ queryKey: keys.subscriptions() });
      context.queryClient.invalidateQueries({ queryKey: keys.grants(true) });
    }
  },
});

function BillingPage() {
  const { purchased, subscribed } = Route.useSearch();

  const { data: balance, isPending: balancePending } = useQuery({
    queryKey: keys.balance(),
    queryFn: fetchBalance,
    staleTime: 5_000,
  });

  const { data: subs, isPending: subsPending } = useQuery({
    queryKey: keys.subscriptions(),
    queryFn: fetchSubscriptions,
    staleTime: 30_000,
  });

  const { data: grants, isPending: grantsPending } = useQuery({
    queryKey: keys.grants(true),
    queryFn: () => fetchGrants(true),
    staleTime: 30_000,
  });

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

      {balancePending ? <BalanceCardSkeleton /> : balance && <BalanceCard balance={balance} />}

      {/* Subscriptions */}
      <div>
        <h2 className="text-lg font-semibold mb-3">Subscriptions</h2>
        <div className="border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-2 font-medium">Plan</th>
                <th className="text-left px-4 py-2 font-medium">Status</th>
                <th className="text-left px-4 py-2 font-medium">Cadence</th>
                <th className="text-left px-4 py-2 font-medium">
                  Period End
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {subsPending ? (
                <SkeletonRows cols={4} />
              ) : subs?.subscriptions?.length ? (
                subs.subscriptions.map((s) => (
                  <tr key={s.subscription_id}>
                    <td className="px-4 py-2 font-medium">{s.plan_id}</td>
                    <td className="px-4 py-2">
                      <StatusPill status={s.status} />
                    </td>
                    <td className="px-4 py-2">{s.cadence}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {s.current_period_end
                        ? new Date(s.current_period_end).toLocaleDateString()
                        : "--"}
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td
                    colSpan={4}
                    className="px-4 py-6 text-center text-muted-foreground"
                  >
                    No active subscriptions.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Credit Grants */}
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
              {grantsPending ? (
                <SkeletonRows cols={4} />
              ) : grants?.grants?.length ? (
                grants.grants.map((g) => (
                  <tr key={g.grant_id}>
                    <td className="px-4 py-2">{g.source}</td>
                    <td className="px-4 py-2 font-mono">
                      {g.amount.toLocaleString()}
                    </td>
                    <td className="px-4 py-2">{g.product_id}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {g.expires_at
                        ? new Date(g.expires_at).toLocaleDateString()
                        : "Never"}
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td
                    colSpan={4}
                    className="px-4 py-6 text-center text-muted-foreground"
                  >
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

function BalanceCardSkeleton() {
  return (
    <div className="border border-border rounded-lg p-6 space-y-3">
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-10 w-44" />
      <div className="flex gap-6 mt-3">
        <Skeleton className="h-4 w-20" />
        <Skeleton className="h-4 w-24" />
      </div>
    </div>
  );
}

function SkeletonRows({ cols, rows = 2 }: { cols: number; rows?: number }) {
  return (
    <>
      {Array.from({ length: rows }).map((_, r) => (
        <tr key={r}>
          {Array.from({ length: cols }).map((_, c) => (
            <td key={c} className="px-4 py-2">
              <Skeleton className="h-4 w-full max-w-[100px]" />
            </td>
          ))}
        </tr>
      ))}
    </>
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
