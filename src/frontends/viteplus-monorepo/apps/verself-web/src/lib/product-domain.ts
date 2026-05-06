import { requireProductDomain } from "@verself/web-env";

// Product domain is a deploy-time constant (set by the Nomad job env), not a
// per-request value. Public docs and policy pages used to fetch it through a
// per-route createServerFn loader, which produced an HTTP RPC and a fake
// pending overlay on every client navigation. Read it once at module init on
// the server and once at hydration on the client by reading the SSR-emitted
// meta tag.
export const PRODUCT_DOMAIN_META_NAME = "x-verself-product-domain";

let cached: string | null = null;

export function readProductDomain(): string {
  if (cached !== null) return cached;
  if (typeof window === "undefined") {
    cached = requireProductDomain();
    return cached;
  }
  const meta = document.querySelector(`meta[name="${PRODUCT_DOMAIN_META_NAME}"]`);
  const content = meta?.getAttribute("content");
  if (!content) {
    throw new Error(`${PRODUCT_DOMAIN_META_NAME} meta tag missing from document`);
  }
  cached = content;
  return cached;
}
