import { createServerFn } from "@tanstack/react-start";

// Resolved on the server so client navigations don't need process.env access.
export const getSiteOrigin = createServerFn({ method: "GET" }).handler((): string => {
  const explicit = process.env.SITE_ORIGIN?.trim();
  if (explicit) return explicit.replace(/\/$/, "");

  const domain = process.env.FORGE_METAL_DOMAIN?.trim();
  if (domain) {
    const subdomain = process.env.LETTERS_SUBDOMAIN?.trim() || "letters";
    return `https://${subdomain}.${domain}`;
  }

  const port = process.env.PORT?.trim() || "4247";
  const host = process.env.HOST?.trim() || "127.0.0.1";
  return `http://${host}:${port}`;
});

export function formatPublishedDate(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return new Intl.DateTimeFormat("en-US", {
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  }).format(parsed);
}
