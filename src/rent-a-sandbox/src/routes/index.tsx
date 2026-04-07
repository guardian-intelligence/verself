import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { getUser } from "~/lib/auth";
import { fetchBalance } from "~/lib/api";
import { keys } from "~/lib/query-keys";
import { BalanceCard } from "~/components/balance-card";

export const Route = createFileRoute("/")({
  component: Dashboard,
  validateSearch: (search: Record<string, unknown>) => ({
    purchased: search.purchased === true || search.purchased === "true",
    subscribed: search.subscribed === true || search.subscribed === "true",
  }),
});

function Dashboard() {
  const { purchased, subscribed } = Route.useSearch();
  const queryClient = useQueryClient();
  const [mounted, setMounted] = useState(false);
  const [user, setUser] = useState<Awaited<ReturnType<typeof getUser>>>(null);

  useEffect(() => {
    setMounted(true);
    getUser().then(setUser);
  }, []);

  // Immediately refetch balance after Stripe redirect
  useEffect(() => {
    if ((purchased || subscribed) && mounted) {
      queryClient.invalidateQueries({ queryKey: keys.balance() });
      queryClient.invalidateQueries({ queryKey: keys.subscriptions() });
      queryClient.invalidateQueries({ queryKey: keys.grants(true) });
    }
  }, [purchased, subscribed, mounted, queryClient]);

  const { data: balance } = useQuery({
    queryKey: keys.balance(),
    queryFn: fetchBalance,
    staleTime: 5_000,
    enabled: mounted && !!user,
  });

  if (!mounted) return null;

  if (!user) {
    return (
      <div className="space-y-8">
        <h1 className="text-2xl font-bold">Rent-a-Sandbox</h1>
        <div className="border border-border rounded-lg p-8 text-center space-y-3">
          <p className="text-lg text-muted-foreground">
            Firecracker CI sandboxes on bare metal.
          </p>
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

      {balance && <BalanceCard balance={balance} />}

      {balance && balance.total_available <= 0 && (
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

      <div className="grid md:grid-cols-2 gap-4">
        <Link
          to="/jobs"
          className="border border-border rounded-lg p-6 hover:bg-accent/50 transition-colors"
        >
          <h3 className="font-semibold mb-1">Sandboxes</h3>
          <p className="text-sm text-muted-foreground">
            Create and monitor Firecracker CI sandboxes
          </p>
        </Link>
        <Link
          to="/billing"
          className="border border-border rounded-lg p-6 hover:bg-accent/50 transition-colors"
        >
          <h3 className="font-semibold mb-1">Billing</h3>
          <p className="text-sm text-muted-foreground">
            Manage subscriptions, credits, and grants
          </p>
        </Link>
      </div>
    </div>
  );
}
