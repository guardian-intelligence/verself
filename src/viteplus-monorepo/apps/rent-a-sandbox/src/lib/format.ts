import { formatUTCDateTime as formatStableUTCDateTime } from "@forge-metal/web-env";

const numberFormatter = new Intl.NumberFormat("en-US");

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
