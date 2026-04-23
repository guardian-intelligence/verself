import { createRootRoute, HeadContent, Outlet, Scripts } from "@tanstack/react-router";
import { BrandTelemetryProvider } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { TelemetryProbe } from "~/lib/telemetry/page-view";
import { deployMetaTags } from "~/lib/telemetry/server-deploy-meta";
import "~/styles/app.css";

// __root — the HTML shell. No chrome, no footer, no data-treatment wrapper.
// Each of the three layout roots (_workshop, letters, newsroom) imports its
// own AppChrome + footer and sets its own data-treatment scope, so the chrome
// is determined by which URL the visitor is on, not by a route-metadata hook.
//
// Routes outside those three layouts (og/$slug, sitemap.xml, api/*) render
// straight into this shell without a chrome — correct, because they are
// machine-readable responses (SVG, XML, JSON), not HTML pages.

export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#0E0E0E" },
      { property: "og:site_name", content: "Guardian" },
      ...deployMetaTags(),
    ],
    links: [
      { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
      { rel: "alternate icon", type: "image/x-icon", href: "/favicon.ico" },
      { rel: "apple-touch-icon", sizes: "180x180", href: "/apple-touch-icon.png" },
      { rel: "manifest", href: "/site.webmanifest" },
      { rel: "sitemap", type: "application/xml", href: "/sitemap.xml" },
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
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="text-foreground font-sans antialiased">
        <a
          href="#main"
          className="sr-only-focusable"
          style={{
            position: "fixed",
            top: "0.75rem",
            left: "0.75rem",
            zIndex: 50,
            padding: "0.5rem 0.75rem",
            background: "var(--color-amber)",
            color: "var(--color-ink)",
            fontFamily: "'Geist', sans-serif",
            fontWeight: 500,
            fontSize: "13px",
            borderRadius: "4px",
          }}
        >
          Skip to main content
        </a>
        <BrandTelemetryProvider emitSpan={emitSpan}>
          <Outlet />
        </BrandTelemetryProvider>
        <TelemetryProbe />
        <Scripts />
      </body>
    </html>
  );
}
