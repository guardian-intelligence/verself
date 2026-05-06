export type BuildsSearch = {
  readonly from: string;
  readonly to: string;
  readonly tz: string;
};

export const DEFAULT_BUILDS_TIME_ZONE = "America/Los_Angeles";

const dateOnlyPattern = /^\d{4}-\d{2}-\d{2}$/;

export function parseBuildsSearch(search: Record<string, unknown>): BuildsSearch {
  return {
    from: parseDateOnly(search.from),
    to: parseDateOnly(search.to),
    tz: parseTimeZone(search.tz),
  };
}

export function canonicalBuildsSearch(search: BuildsSearch, now: Date = new Date()): BuildsSearch {
  const tz = search.tz || DEFAULT_BUILDS_TIME_ZONE;
  const today = dateInTimeZone(now, tz);
  const from = search.from || search.to || today;
  const to = search.to || from;
  return from <= to ? { from, to, tz } : { from: to, to: from, tz };
}

export function sameBuildsSearch(left: BuildsSearch, right: BuildsSearch): boolean {
  return left.from === right.from && left.to === right.to && left.tz === right.tz;
}

export function buildsSearchHref(pathname: string, search: BuildsSearch): string {
  const params = new URLSearchParams();
  params.set("from", search.from);
  params.set("to", search.to);
  params.set("tz", search.tz);
  return `${pathname}?${params.toString()}`;
}

export function dateFromDateOnly(value: string): Date {
  const match = dateOnlyPattern.exec(value);
  if (!match) {
    throw new Error(`invalid date-only value: ${value}`);
  }
  const [, yearText, monthText, dayText] = match;
  if (!yearText || !monthText || !dayText) {
    throw new Error(`invalid date-only value: ${value}`);
  }
  const year = Number.parseInt(yearText, 10);
  const month = Number.parseInt(monthText, 10);
  const day = Number.parseInt(dayText, 10);
  return new Date(Date.UTC(year, month - 1, day));
}

function parseDateOnly(value: unknown): string {
  if (typeof value !== "string" || !isValidDateOnly(value)) return "";
  return value;
}

function isValidDateOnly(value: string): boolean {
  if (!dateOnlyPattern.test(value)) return false;
  return dateFromDateOnly(value).toISOString().slice(0, 10) === value;
}

function parseTimeZone(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) return "";
  const candidate = value.trim();
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: candidate }).format(new Date(0));
    return candidate;
  } catch {
    return "";
  }
}

function dateInTimeZone(value: Date, timeZone: string): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    day: "2-digit",
    month: "2-digit",
    timeZone,
    year: "numeric",
  }).formatToParts(value);
  const year = requiredDatePart(parts, "year");
  const month = requiredDatePart(parts, "month");
  const day = requiredDatePart(parts, "day");
  return `${year}-${month}-${day}`;
}

function requiredDatePart(parts: Intl.DateTimeFormatPart[], type: Intl.DateTimeFormatPartTypes) {
  const value = parts.find((part) => part.type === type)?.value;
  if (!value) {
    throw new Error(`date formatter did not return ${type}`);
  }
  return value;
}
