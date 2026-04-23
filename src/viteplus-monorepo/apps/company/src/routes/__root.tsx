import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import { AppChrome, BrandTelemetryProvider } from "@forge-metal/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { useCurrentTreatment } from "~/lib/treatment";
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
    <RootDocument>
      <Outlet />
    </RootDocument>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  const treatment = useCurrentTreatment();
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
            background: "var(--color-flare)",
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
          <div
            data-treatment={treatment}
            className="flex min-h-svh flex-col transition-colors duration-300 ease-out"
            style={{
              background: "var(--treatment-ground)",
              color: "var(--treatment-ink)",
            }}
          >
            <AppChrome treatment={treatment} LinkComponent={LinkAdapter} />
            <main id="main" className="flex-1">
              {children}
            </main>
            <SiteFooter />
          </div>
        </BrandTelemetryProvider>
        <TelemetryProbe />
        <Scripts />
      </body>
    </html>
  );
}

// Thin adapter wrapping TanStack's typed <Link> so AppChrome (which is
// router-agnostic and accepts a generic LinkLikeProps) stays inside the
// brand package. The `to` prop is typed loosely at the brand boundary but
// TanStack's Link at this site has well-known destinations; any string is
// fine because the wordmark only ever points home.
function LinkAdapter(props: {
  to: string;
  className?: string;
  style?: React.CSSProperties;
  "aria-label"?: string;
  onClick?: React.MouseEventHandler;
  children?: ReactNode;
}) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return <Link {...(props as any)} />;
}

// Company-site footer. Three columns mirror the public IA. Terms, privacy,
// SLA, security disclosures, and every legal surface that binds a customer
// live with Metal Platform — the product that actually processes the data —
// and are not duplicated here.
function SiteFooter() {
  return (
    <footer
      className="mt-16 transition-colors duration-300 ease-out"
      style={{
        background: "var(--treatment-ground)",
        color: "var(--treatment-ink)",
        borderTop: "1px solid var(--treatment-hairline)",
      }}
    >
      <div className="mx-auto grid w-full max-w-7xl gap-8 px-4 py-10 md:grid-cols-3 md:px-6">
        <FooterColumn heading="Company">
          <FooterLink to="/company">About</FooterLink>
          <FooterLink to="/careers">Careers</FooterLink>
          <FooterLink to="/changelog">Changelog</FooterLink>
          <FooterLink to="/contact">Contact</FooterLink>
        </FooterColumn>
        <FooterColumn heading="Solutions">
          <FooterLink to="/solutions">Overview</FooterLink>
          <FooterExternal href="https://platform.anveio.com">Metal Platform</FooterExternal>
        </FooterColumn>
        <FooterColumn heading="Read">
          <FooterLink to="/letters">Letters</FooterLink>
          <FooterExternal href="/letters/rss">RSS</FooterExternal>
          <FooterLink to="/design">Design system</FooterLink>
          <FooterLink to="/press">Press kit</FooterLink>
        </FooterColumn>
      </div>
      <div
        className="mx-auto w-full max-w-7xl px-4 pb-10 md:px-6"
        style={{
          fontFamily: "'Geist Mono', ui-monospace, monospace",
          fontSize: "11px",
          letterSpacing: "0.12em",
          textTransform: "uppercase",
          color: "var(--treatment-muted-faint)",
        }}
      >
        © 2026 Guardian Intelligence LLC · Seattle, Washington
      </div>
    </footer>
  );
}

function FooterColumn({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <p
        className="font-mono text-[10px] font-medium uppercase tracking-[0.18em]"
        style={{ color: "var(--treatment-muted-faint)" }}
      >
        {heading}
      </p>
      <ul className="flex flex-col gap-2 text-sm">{children}</ul>
    </div>
  );
}

function FooterLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <li>
      <Link
        to={to}
        className="transition-colors hover:underline hover:underline-offset-4"
        style={{ color: "var(--treatment-muted)" }}
      >
        {children}
      </Link>
    </li>
  );
}

function FooterExternal({ href, children }: { href: string; children: ReactNode }) {
  return (
    <li>
      <a
        href={href}
        className="transition-colors hover:underline hover:underline-offset-4"
        style={{ color: "var(--treatment-muted)" }}
      >
        {children}
      </a>
    </li>
  );
}
