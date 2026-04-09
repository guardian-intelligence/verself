// Keep these names aligned with @forge-metal/nitro-plugins/src/correlation.ts.
// The browser must not import the Nitro package root or it will pull server
// middleware helpers into the client bundle.
export const correlationCookieName = "fm_correlation_id";
export const correlationHeaderName = "X-Forge-Metal-Correlation-Id";

export function readBrowserCookie(name: string): string {
  if (typeof document === "undefined") {
    return "";
  }
  const prefix = `${encodeURIComponent(name)}=`;
  for (const part of document.cookie.split(";")) {
    const trimmed = part.trim();
    if (!trimmed.startsWith(prefix)) {
      continue;
    }
    return decodeURIComponent(trimmed.slice(prefix.length));
  }
  return "";
}
