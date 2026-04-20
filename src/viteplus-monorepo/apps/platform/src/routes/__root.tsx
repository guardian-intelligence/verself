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
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Forge Metal Platform" },
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

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body className="bg-background text-foreground font-sans antialiased">
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

// Minimal top bar — Guardian wordmark only. Section navigation belongs to the
// page it applies to: /docs owns its own left rail for docs sections, /policy
// and /design own their own. Duplicating those links up here would just confuse
// the hierarchy.
function TopBar() {
  return (
    <header className="sticky top-0 z-30 border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-7xl items-center px-4 md:px-6">
        <Link
          to="/"
          aria-label="Forge Metal Platform — home"
          className="inline-flex items-center"
          style={{ color: "currentColor" }}
        >
          <Lockup size="sm" wordmark="Forge Metal" variant="chip" title="Forge Metal Platform" />
        </Link>
      </div>
    </header>
  );
}

// Site footer — the discoverability surface for policy + design surfaces. Living
// in the root layout so every page (docs, reference, policy, marketing, design)
// shows the same set of legal and reference links without each page wiring them.
function SiteFooter() {
  return (
    <footer className="mt-16 border-t border-border bg-background/60">
      <div className="mx-auto grid w-full max-w-7xl gap-8 px-4 py-10 md:grid-cols-4 md:px-6">
        <FooterColumn heading="Platform">
          <FooterLink to="/docs">Docs</FooterLink>
          <FooterLink to="/docs/reference">API Reference</FooterLink>
        </FooterColumn>
        <FooterColumn heading="Brand">
          <FooterLink to="/design">Design system</FooterLink>
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
    </footer>
  );
}

function FooterColumn({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{heading}</p>
      <ul className="flex flex-col gap-2 text-sm">{children}</ul>
    </div>
  );
}

function FooterLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <li>
      <Link
        to={to}
        className="text-muted-foreground transition-colors hover:text-foreground hover:underline hover:underline-offset-4"
      >
        {children}
      </Link>
    </li>
  );
}
