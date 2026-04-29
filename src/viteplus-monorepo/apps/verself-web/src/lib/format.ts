import { formatUTCDateTime as formatStableUTCDateTime } from "@verself/web-env";

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
const centsFormatters = new Map<string, Intl.NumberFormat>();

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

const localDateTimeFormatter = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

// Renders a timestamp in the viewer's detected IANA time zone. Callers gate
// this behind a hydration-safe boundary — server rendering uses UTC and
// swaps on first client render so SSR/CSR markup stays consistent.
export function formatDateTimeLocal(value: Date | number | string): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "Invalid timestamp";
  return localDateTimeFormatter.format(date);
}

export function formatRelative(value: Date | number | string, now: Date = new Date()): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const deltaSeconds = Math.round((now.getTime() - date.getTime()) / 1000);
  if (Math.abs(deltaSeconds) < 60) return `${deltaSeconds}s ago`;
  const deltaMinutes = Math.round(deltaSeconds / 60);
  if (Math.abs(deltaMinutes) < 60) return `${deltaMinutes}m ago`;
  const deltaHours = Math.round(deltaMinutes / 60);
  if (Math.abs(deltaHours) < 24) return `${deltaHours}h ago`;
  const deltaDays = Math.round(deltaHours / 24);
  if (Math.abs(deltaDays) < 30) return `${deltaDays}d ago`;
  const deltaMonths = Math.round(deltaDays / 30);
  if (Math.abs(deltaMonths) < 12) return `${deltaMonths}mo ago`;
  const deltaYears = Math.round(deltaMonths / 12);
  return `${deltaYears}y ago`;
}

export function formatDateTimeMillisUTC(value: Date | number | string): string {
  return `${formatStableUTCDateTime(
    value,
    {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      fractionalSecondDigits: 3,
      hour12: false,
    },
    { invalid: "Invalid timestamp", locale: "sv-SE" },
  )} UTC`;
}

export function formatInteger(value: number): string {
  return numberFormatter.format(value);
}

export function formatCents(value: number, currency: string): string {
  const normalizedCurrency = currency.toUpperCase();
  const key = `en-US:${normalizedCurrency}`;
  let formatter = centsFormatters.get(key);
  if (!formatter) {
    formatter = new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: normalizedCurrency,
    });
    centsFormatters.set(key, formatter);
  }
  return formatter.format(value / 100);
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
