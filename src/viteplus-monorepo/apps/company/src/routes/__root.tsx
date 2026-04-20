import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import { Lockup } from "@forge-metal/brand";
import { TelemetryProbe } from "~/lib/telemetry/page-view";
import { deployMetaTags } from "~/lib/telemetry/server-deploy-meta";
import "~/styles/app.css";

export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#0E0E0E" },
      { property: "og:site_name", content: "Guardian Intelligence" },
      ...deployMetaTags(),
    ],
    links: [
      { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
      { rel: "alternate icon", type: "image/x-icon", href: "/favicon.ico" },
      { rel: "apple-touch-icon", sizes: "180x180", href: "/apple-touch-icon.png" },
      { rel: "manifest", href: "/site.webmanifest" },
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

function RootComponent() {
  return (
    <RootDocument>
      <Outlet />
    </RootDocument>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body
        className="text-foreground font-sans antialiased"
        style={{ background: "var(--color-iron)", color: "var(--color-type-iron)" }}
      >
        <div className="flex min-h-svh flex-col">
          <TopBar />
          <main id="main" className="flex-1">
            {children}
          </main>
          <SiteFooter />
        </div>
        <TelemetryProbe />
        <Scripts />
      </body>
    </html>
  );
}

function TopBar() {
  return (
    <header
      className="sticky top-0 z-30"
      style={{
        background: "var(--color-iron)",
        borderBottom: "1px solid rgba(245,245,245,0.08)",
      }}
    >
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-7xl items-center px-4 md:px-6">
        <Link
          to="/"
          aria-label="Guardian Intelligence — home"
          className="inline-flex items-center"
          style={{ color: "var(--color-type-iron)" }}
        >
          <Lockup size="sm" wordmark="Guardian Intelligence" title="Guardian Intelligence" />
        </Link>
      </div>
    </header>
  );
}

// Phase 1 ships a minimal footer. Phase 4 populates the company-site footer
// with the IA (Dispatch / Products / Trust / Press / …) once those routes
// exist. The legal tree for Metal lives on the platform app and is not
// duplicated here.
function SiteFooter() {
  return (
    <footer
      className="mt-16"
      style={{
        background: "var(--color-iron)",
        color: "var(--color-type-iron)",
        borderTop: "1px solid rgba(245,245,245,0.08)",
      }}
    >
      <div
        className="mx-auto w-full max-w-7xl px-4 py-10 md:px-6"
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontSize: "11px",
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "rgba(245,245,245,0.4)",
        }}
      >
        © 2026 Guardian Intelligence · Seattle, Washington
      </div>
    </footer>
  );
}
