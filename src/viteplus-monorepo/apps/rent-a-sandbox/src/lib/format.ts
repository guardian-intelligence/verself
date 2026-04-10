const numberFormatter = new Intl.NumberFormat("en-US");

// Locale-sensitive Date rendering drifts between SSR and hydration. Keep
// browser-visible timestamps deterministic and UTC-normalized.
export function formatDateUTC(value: Date | number | string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Invalid date";
  }

  return `${date.getUTCFullYear()}-${pad2(date.getUTCMonth() + 1)}-${pad2(date.getUTCDate())}`;
}

export function formatDateTimeUTC(value: Date | number | string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Invalid timestamp";
  }

  return `${formatDateUTC(date)} ${pad2(date.getUTCHours())}:${pad2(date.getUTCMinutes())} UTC`;
}

export function formatInteger(value: number): string {
  return numberFormatter.format(value);
}

function pad2(value: number): string {
  return value.toString().padStart(2, "0");
}
