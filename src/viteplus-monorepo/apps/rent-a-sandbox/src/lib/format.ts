import { formatUTCDateTime as formatStableUTCDateTime } from "@forge-metal/web-env";

const numberFormatter = new Intl.NumberFormat("en-US");
const ledgerUnitsPerUSD = 10_000_000;
const ledgerMoneyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});
const ledgerPreciseMoneyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 6,
});

export function formatDateUTC(value: Date | number | string): string {
  return formatStableUTCDateTime(
    value,
    { year: "numeric", month: "2-digit", day: "2-digit" },
    { invalid: "Invalid date", locale: "sv-SE" },
  );
}

export function formatDateTimeUTC(value: Date | number | string): string {
  return `${formatStableUTCDateTime(
    value,
    {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    },
    { invalid: "Invalid timestamp", locale: "sv-SE" },
  )} UTC`;
}

export function formatInteger(value: number): string {
  return numberFormatter.format(value);
}

export function formatLedgerAmount(value: number): string {
  // Stripe purchases store cents * 100,000, so 10,000,000 ledger units equals one USD.
  return ledgerMoneyFormatter.format(value / ledgerUnitsPerUSD);
}

export function formatLedgerAmountPrecise(value: number): string {
  return ledgerPreciseMoneyFormatter.format(value / ledgerUnitsPerUSD);
}

export function formatLedgerRate(value: number, quantityUnit = "unit"): string {
  return `${formatLedgerAmountPrecise(value)} / ${quantityUnit}`;
}
