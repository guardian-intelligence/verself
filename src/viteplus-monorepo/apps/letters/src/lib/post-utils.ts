import type { JsonValue } from "~/server-fns/validation";

export function parsePostTags(tags: unknown): string[] {
  if (!tags) return [];
  if (Array.isArray(tags)) {
    return tags.filter((tag): tag is string => typeof tag === "string" && tag.length > 0);
  }
  if (typeof tags !== "string") {
    return [];
  }
  if (tags.startsWith("{") && tags.endsWith("}")) {
    return tags.slice(1, -1).split(",").filter(Boolean);
  }
  try {
    const parsed = JSON.parse(tags);
    return Array.isArray(parsed)
      ? parsed.filter((tag): tag is string => typeof tag === "string" && tag.length > 0)
      : [];
  } catch {
    return [];
  }
}

export function parsePostContent(content: unknown): JsonValue {
  if (typeof content !== "string") {
    return (content ?? null) as JsonValue;
  }
  try {
    return JSON.parse(content) as JsonValue;
  } catch {
    return content;
  }
}

const SHORT_POST_DATE_FORMATTER = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

const LONG_POST_DATE_FORMATTER = new Intl.DateTimeFormat("en-US", {
  month: "long",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

export function formatPostDate(dateStr: string | null, style: "short" | "long" = "short"): string {
  if (!dateStr) return "";
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return "";
  // Pin the timezone so SSR and hydration render the same calendar date.
  return (style === "long" ? LONG_POST_DATE_FORMATTER : SHORT_POST_DATE_FORMATTER).format(date);
}

export function sortPostsByPublishedAt<
  TPost extends { created_at: string; published_at: string | null },
>(posts: readonly TPost[]): TPost[] {
  return [...posts].sort(
    (left, right) =>
      new Date(right.published_at ?? right.created_at).getTime() -
      new Date(left.published_at ?? left.created_at).getTime(),
  );
}

export function sortPostsByUpdatedAt<TPost extends { updated_at: string }>(
  posts: readonly TPost[],
): TPost[] {
  return [...posts].sort(
    (left, right) => new Date(right.updated_at).getTime() - new Date(left.updated_at).getTime(),
  );
}
