import { createRootRouteWithContext, HeadContent, Outlet, Scripts } from "@tanstack/react-router";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { AuthProvider } from "@verself/auth-web/react";
import { type Auth, anonymousSnapshot } from "@verself/auth-web/isomorphic";
import { BrandTelemetryProvider } from "@verself/brand";
import { Toaster } from "@verself/ui/components/ui/sonner";
import { emitSpan } from "~/lib/telemetry/browser";
import { TelemetryProbe } from "~/lib/telemetry/page-view";
import { deployMetaTags } from "~/lib/telemetry/server-deploy-meta";
import { PRODUCT_DOMAIN_META_NAME, readProductDomain } from "~/lib/product-domain";
import { DevelopmentModeHotkey } from "~/components/development-mode-hotkey";
import "~/styles/app.css";

const authNavigationClient = {
  getSignInRedirectURL: async ({
    data,
  }: {
    data: {
      promptLogin?: boolean | null;
      promptSelectAccount?: boolean | null;
      redirectTo?: string | null;
      purpose?: string | null;
      loginHint?: string | null;
      requiredSubject?: string | null;
      requiredEmail?: string | null;
      requiredOrgId?: string | null;
    };
  }) => {
    const params = new URLSearchParams();
    if (data.redirectTo) {
      params.set("redirect", data.redirectTo);
    }
    if (data.purpose) {
      params.set("purpose", data.purpose);
    }
    if (data.loginHint) {
      params.set("login_hint", data.loginHint);
    }
    if (data.requiredSubject) {
      params.set("required_subject", data.requiredSubject);
    }
    if (data.requiredEmail) {
      params.set("required_email", data.requiredEmail);
    }
    if (data.requiredOrgId) {
      params.set("required_org_id", data.requiredOrgId);
    }
    if (data.promptLogin) {
      params.set("prompt", "login");
    } else if (data.promptSelectAccount) {
      params.set("prompt", "select_account");
    }
    const query = params.toString();
    return `/login${query ? `?${query}` : ""}`;
  },
  getSignOutRedirectURL: async () => "/api/v1/auth/logout",
};

// Root provides an anonymous AuthProvider so public surfaces (docs, policy,
// /login) render without a server round-trip. Routes that need the real
// snapshot (the signed-in shell, /login's redirect-if-already-signed-in
// guard) fetch it themselves and re-wrap their subtree in `AuthProvider`.
export const Route = createRootRouteWithContext<{
  auth: Auth;
  queryClient: QueryClient;
}>()({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#000000" },
      { property: "og:site_name", content: "Verself" },
      { title: "Verself" },
      // Emit the product domain into SSR HTML once so workshop pages can
      // render API/origin examples without a per-route createServerFn round
      // trip. Read by ~/lib/product-domain.ts on the client.
      { name: PRODUCT_DOMAIN_META_NAME, content: readProductDomain() },
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

// __root.tsx owns the document and global providers (anonymous auth + query +
// brand telemetry). Visual chrome lives in pathless route layers: signed-in
// surfaces nest under _shell and re-wrap with the authenticated AuthProvider;
// public docs + policy nest under _workshop and inherit anonymous auth from
// here. iam-service owns the OIDC callback.
function RootComponent() {
  const routeContext = Route.useRouteContext();
  return (
    <QueryClientProvider client={routeContext.queryClient}>
      <AuthProvider client={authNavigationClient} snapshot={anonymousSnapshot}>
        <BrandTelemetryProvider emitSpan={emitSpan}>
          <RootDocument>
            <Outlet />
          </RootDocument>
        </BrandTelemetryProvider>
      </AuthProvider>
    </QueryClientProvider>
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
        <DevelopmentModeHotkey />
        <Toaster />
        <TelemetryProbe />
        <Scripts />
      </body>
    </html>
  );
}
