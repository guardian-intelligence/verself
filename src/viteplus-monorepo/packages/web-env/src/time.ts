export type BrowserVisibleDateInput = Date | number | string;

export type UTCDateTimeFormatConfig = {
  invalid?: string;
  locale?: string;
};

export type UTCDateTimeFormatOptions = Omit<Intl.DateTimeFormatOptions, "timeZone">;

const formatterCache = new Map<string, Intl.DateTimeFormat>();

export function formatUTCDateTime(
  value: BrowserVisibleDateInput,
  options: UTCDateTimeFormatOptions,
  config: UTCDateTimeFormatConfig = {},
): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return config.invalid ?? "";
  }

  return getFormatter(config.locale ?? "en-US", options).format(date);
}

function getFormatter(locale: string, options: UTCDateTimeFormatOptions): Intl.DateTimeFormat {
  const cacheKey = JSON.stringify([locale, stableOptionEntries(options)]);
  const cached = formatterCache.get(cacheKey);
  if (cached) {
    return cached;
  }

  const formatter = new Intl.DateTimeFormat(locale, {
    ...options,
    timeZone: "UTC",
  });
  formatterCache.set(cacheKey, formatter);
  return formatter;
}

function stableOptionEntries(options: UTCDateTimeFormatOptions): Array<[string, unknown]> {
  return Object.entries(options).sort(([left], [right]) => left.localeCompare(right));
}
