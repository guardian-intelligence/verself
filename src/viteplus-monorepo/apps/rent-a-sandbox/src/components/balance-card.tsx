import type { Balance } from "~/server-fns/api";
import { formatLedgerAmount } from "~/lib/format";

const lowCreditThreshold = 10_000_000;

export function BalanceCard({ balance }: { balance: Balance }) {
  const total = balance.total_available;
  const color =
    total <= 0
      ? "border-destructive/50 bg-destructive/5"
      : total < lowCreditThreshold
        ? "border-warning/50 bg-warning/5"
        : "border-success/50 bg-success/5";

  return (
    <div data-testid="balance-card" className={`border rounded-lg p-6 ${color}`}>
      <div className="text-sm text-muted-foreground mb-1">Available Credit Value</div>
      <div data-testid="balance-total" className="text-4xl font-bold font-mono tabular-nums">
        {formatLedgerAmount(total)}
      </div>
      <div className="mt-3 flex gap-6 text-sm text-muted-foreground">
        <div>
          <span className="font-medium text-foreground">
            {formatLedgerAmount(balance.free_tier_available)}
          </span>{" "}
          free tier
        </div>
        <div>
          <span className="font-medium text-foreground">
            {formatLedgerAmount(balance.credit_available)}
          </span>{" "}
          funded
        </div>
        {balance.credit_pending > 0 && (
          <div>
            <span className="font-medium text-foreground">
              {formatLedgerAmount(balance.credit_pending)}
            </span>{" "}
            pending
          </div>
        )}
      </div>
    </div>
  );
}
