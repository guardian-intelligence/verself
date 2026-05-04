import { createRootRouteWithContext, HeadContent, Outlet, Scripts } from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { AuthProvider } from "@verself/auth-web/react";
import {
  type Auth,
  type AuthSnapshot,
  authCacheKey,
  parseAuthSnapshot,
  syncAuthPartitionedCache,
} from "@verself/auth-web/isomorphic";
import { BrandTelemetryProvider } from "@verself/brand";
import { getClientAuthSnapshot } from "~/server-fns/auth";
import { emitSpan } from "~/lib/telemetry/browser";
import { TelemetryProbe } from "~/lib/telemetry/page-view";
import { deployMetaTags } from "~/lib/telemetry/server-deploy-meta";
import "~/styles/app.css";

async function loadAuthSnapshot(): Promise<AuthSnapshot> {
  return getClientAuthSnapshot();
}

const authNavigationClient = {
  getSignInRedirectURL: async ({ data }: { data: { redirectTo?: string | null } }) => {
    const params = new URLSearchParams();
    if (data.redirectTo) {
      params.set("redirect_to", data.redirectTo);
    }
    const query = params.toString();
    return `/api/v1/auth/login${query ? `?${query}` : ""}`;
  },
  getSignOutRedirectURL: async () => "/api/v1/auth/logout",
};

export const Route = createRootRouteWithContext<{
  auth: Auth;
  queryClient: QueryClient;
}>()({
  component: RootComponent,
  beforeLoad: async ({ context }) => {
    const authSnapshot = await loadAuthSnapshot();
    syncAuthPartitionedCache(context.queryClient, authSnapshot);
    return authSnapshot;
  },
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Verself" },
      { title: "Verself" },
      ...deployMetaTags(),
    ],
    links: [
      { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
      { rel: "alternate icon", type: "image/x-icon", href: "/favicon.ico" },
      { rel: "apple-touch-icon", sizes: "180x180", href: "/apple-touch-icon.png" },
      { rel: "manifest", href: "/site.webmanifest" },
      // Self-hosted variable WOFF2 — see styles/app.css and CSP `font-src 'self' data:`.
      // Preload the headline + body axes so first paint resolves to Fraunces/Geist
      // instead of the system fallback. Geist Mono is loaded lazily as needed.
      {
        rel: "preload",
        href: "/fonts/Fraunces-Variable.woff2",
        as: "font",
        type: "font/woff2",
        crossOrigin: "anonymous",
      },
      {
        rel: "preload",
        href: "/fonts/Geist-Variable.woff2",
        as: "font",
        type: "font/woff2",
        crossOrigin: "anonymous",
      },
    ],
  }),
});

// __root.tsx owns the document and global providers (auth + query + brand
// telemetry). Visual chrome lives in pathless route layers: signed-in surfaces
// nest under _shell/_authenticated; public docs + policy nest under _workshop.
// Auth entry routes render with no chrome; iam-service owns the OIDC callback.
function RootComponent() {
  const routeContext = Route.useRouteContext();
  const authSnapshot = parseAuthSnapshot(routeContext);
  return (
    <AuthProvider client={authNavigationClient} snapshot={authSnapshot}>
      <QueryClientProvider client={routeContext.queryClient} key={authCacheKey(authSnapshot)}>
        <BrandTelemetryProvider emitSpan={emitSpan}>
          <RootDocument>
            <Outlet />
          </RootDocument>
        </BrandTelemetryProvider>
      </QueryClientProvider>
    </AuthProvider>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-background text-foreground font-sans antialiased">
        {children}
        <TelemetryProbe />
        <Scripts />
      </body>
    </html>
  );
}
