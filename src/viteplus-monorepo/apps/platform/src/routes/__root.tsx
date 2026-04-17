import { createRootRoute, HeadContent, Link, Outlet, Scripts } from "@tanstack/react-router";
import { type ReactNode } from "react";
import "~/styles/app.css";

export const Route = createRootRoute({
  component: RootComponent,
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "theme-color", content: "#ffffff" },
      { property: "og:site_name", content: "Forge Metal Platform" },
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
        <Scripts />
      </body>
    </html>
  );
}

// Minimal top bar — brand lockup only. Section navigation belongs to the
// page it applies to: /docs owns its own left rail for docs sections, and
// each docs page owns its own AnchorNav for in-page sections. Duplicating
// those links up here would just confuse the hierarchy.
function TopBar() {
  return (
    <header className="sticky top-0 z-30 border-b border-border bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-[var(--header-h)] w-full max-w-7xl items-center px-4 md:px-6">
        <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
          <span aria-hidden="true" className="text-base">
            ◼
          </span>
          <span className="text-sm">Forge Metal Platform</span>
        </Link>
      </div>
    </header>
  );
}

// Site footer — the discoverability surface for policy documents. Living in
// the root layout so every page (docs, reference, policy, marketing) shows the
// same set of legal and reference links without each page wiring them itself.
function SiteFooter() {
  return (
    <footer className="mt-16 border-t border-border bg-background/60">
      <div className="mx-auto grid w-full max-w-7xl gap-8 px-4 py-10 md:grid-cols-3 md:px-6">
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
