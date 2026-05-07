import { useParams } from "@tanstack/react-router";

export type OrgScopedPath = `/${string}`;

const RESERVED_ROOT_SEGMENTS = new Set([
  "api",
  "docs",
  "login",
  "logout",
  "policy",
  "favicon.ico",
  "site.webmanifest",
]);

export function orgPath(orgSlug: string, path: OrgScopedPath): string {
  if (path === "/") return `/${orgSlug}`;
  return `/${orgSlug}${path}`;
}

export function replaceOrgSlugInHref(href: string, nextOrgSlug: string): string {
  const url = new URL(href, "https://verself.invalid");
  url.pathname = replaceOrgSlugInPath(url.pathname, nextOrgSlug);
  return url.origin === "https://verself.invalid"
    ? `${url.pathname}${url.search}${url.hash}`
    : url.toString();
}

export function stripOrgSlug(pathname: string): string {
  const segments = pathname.split("/").filter(Boolean);
  const [first, ...rest] = segments;
  if (!first || RESERVED_ROOT_SEGMENTS.has(first)) {
    return pathname;
  }
  return rest.length > 0 ? `/${rest.join("/")}` : "/";
}

export function useOrgRouteParams(): { readonly orgSlug: string } {
  const params = useParams({ strict: false }) as { readonly orgSlug?: string };
  if (!params.orgSlug) {
    throw new Error("organization route param is required in the console shell");
  }
  return { orgSlug: params.orgSlug };
}

export function useOptionalOrgSlug(): string | null {
  const params = useParams({ strict: false }) as { readonly orgSlug?: string };
  return params.orgSlug ?? null;
}

export function useOrgScopedPath(path: OrgScopedPath): string {
  const { orgSlug } = useOrgRouteParams();
  return orgPath(orgSlug, path);
}

function replaceOrgSlugInPath(pathname: string, nextOrgSlug: string): string {
  const segments = pathname.split("/").filter(Boolean);
  const [, ...rest] = segments;
  return rest.length > 0 ? `/${nextOrgSlug}/${rest.join("/")}` : `/${nextOrgSlug}`;
}
