import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import { AppChrome, BrandTelemetryProvider } from "@verself/brand";
import { emitSpan } from "~/lib/telemetry/browser";
import { TelemetryProbe } from "~/lib/telemetry/page-view";
import { deployMetaTags } from "~/lib/telemetry/server-deploy-meta";
import "~/styles/app.css";

export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Guardian" },
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

function RootComponent() {
  return (
    <RootDocument>
      <Outlet />
    </RootDocument>
  );
}

// Platform is a single-treatment app — Workshop chrome end-to-end. Docs,
// reference, and policy all share the same engineer-facing register. Routes
// do not override treatment; keeping it a literal eliminates a path-matching
// hook and makes the chrome static at SSR.
function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="text-foreground font-sans antialiased">
        <BrandTelemetryProvider emitSpan={emitSpan}>
          <div
            data-treatment="workshop"
            className="flex min-h-svh flex-col"
            style={{
              background: "var(--treatment-ground)",
              color: "var(--treatment-ink)",
            }}
          >
            <AppChrome treatment="workshop" LinkComponent={LinkAdapter} />
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

// Site footer — the discoverability surface for policy + design surfaces. Living
// in the root layout so every page (docs, reference, policy, marketing, design)
// shows the same set of legal and reference links without each page wiring them.
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
      <div className="mx-auto grid w-full max-w-7xl gap-8 px-4 py-10 md:grid-cols-4 md:px-6">
        <FooterColumn heading="Platform">
          <FooterLink to="/docs">Docs</FooterLink>
          <FooterLink to="/docs/reference">API Reference</FooterLink>
        </FooterColumn>
        <FooterColumn heading="Policy">
          <FooterLink to="/policy">Overview</FooterLink>
          <FooterLink to="/policy/terms">Terms of Service</FooterLink>
          <FooterLink to="/policy/privacy">Privacy Policy</FooterLink>
          <FooterLink to="/policy/acceptable-use">Acceptable Use</FooterLink>
          <FooterLink to="/policy/dpa">Data Processing Addendum</FooterLink>
          <FooterLink to="/policy/subprocessors">Subprocessors</FooterLink>
          <FooterLink to="/policy/sla">SLA</FooterLink>
        </FooterColumn>
        <FooterColumn heading="Trust">
          <FooterLink to="/policy/security">Security</FooterLink>
          <FooterLink to="/policy/data-retention">Data Retention</FooterLink>
          <FooterLink to="/policy/cookies">Cookies</FooterLink>
          <FooterLink to="/policy/changelog">Policy changelog</FooterLink>
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
