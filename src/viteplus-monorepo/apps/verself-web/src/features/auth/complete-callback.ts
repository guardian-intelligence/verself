import type { useRouter } from "@tanstack/react-router";

function searchParamsObject(params: URLSearchParams): Record<string, string | Array<string>> {
  const out: Record<string, string | Array<string>> = {};
  for (const [key, value] of params.entries()) {
    const existing = out[key];
    if (Array.isArray(existing)) {
      existing.push(value);
      continue;
    }
    out[key] = existing === undefined ? value : [existing, value];
  }
  return out;
}

// completeBrowserCallback finishes the OIDC relying-party callback over XHR so the
// SPA stays mounted (no full-page reload). The callback route
// (/api/v1/auth/callback) exchanges the authorization code and sets the session
// cookie; for JSON callers it returns { redirectTo } instead of a 303. Cookies
// ride along (same-origin, credentials included), then the router is invalidated
// so the destination's auth check reads the freshly set session before we
// navigate. Used only for callback URLs returned by the login server functions;
// plain in-app navigations should use the router directly.
export async function completeBrowserCallback(
  router: ReturnType<typeof useRouter>,
  callbackUrl: string,
): Promise<void> {
  const url = new URL(callbackUrl, window.location.origin);
  if (url.origin !== window.location.origin) {
    throw new Error("External auth callback URL rejected.");
  }
  const response = await fetch(url.toString(), {
    method: "GET",
    credentials: "include",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error("Sign in could not be completed.");
  }
  const body = (await response.json()) as { readonly redirectTo?: string };
  const destination = new URL(body.redirectTo ?? "/", window.location.origin);
  if (destination.origin !== window.location.origin) {
    throw new Error("External auth redirect rejected.");
  }
  await router.invalidate();
  await router.navigate({
    to: destination.pathname,
    search: searchParamsObject(destination.searchParams),
    ...(destination.hash ? { hash: destination.hash.slice(1) } : {}),
  } as never);
}
