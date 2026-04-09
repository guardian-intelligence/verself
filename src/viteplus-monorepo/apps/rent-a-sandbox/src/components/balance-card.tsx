import type { Balance } from "~/server-fns/api";

export function BalanceCard({ balance }: { balance: Balance }) {
  const total = balance.total_available;
  const color =
    total <= 0
      ? "border-destructive/50 bg-destructive/5"
      : total < 1000
        ? "border-warning/50 bg-warning/5"
        : "border-success/50 bg-success/5";

  return (
    <div className={`border rounded-lg p-6 ${color}`}>
      <div className="text-sm text-muted-foreground mb-1">Available Credits</div>
      <div className="text-4xl font-bold font-mono tabular-nums">{total.toLocaleString()}</div>
      <div className="mt-3 flex gap-6 text-sm text-muted-foreground">
        <div>
          <span className="font-medium text-foreground">
            {balance.free_tier_available.toLocaleString()}
          </span>{" "}
          free tier
        </div>
        <div>
          <span className="font-medium text-foreground">
            {balance.credit_available.toLocaleString()}
          </span>{" "}
          purchased
        </div>
        {balance.credit_pending > 0 && (
          <div>
            <span className="font-medium text-foreground">
              {balance.credit_pending.toLocaleString()}
            </span>{" "}
            pending
          </div>
        )}
      </div>
    </div>
  );
}
