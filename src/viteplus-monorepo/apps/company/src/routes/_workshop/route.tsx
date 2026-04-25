import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AppChrome } from "@verself/brand";

// _workshop — Guardian's default layout. Every URL that is not /letters/* or
// /newsroom lives here: /, /company, /careers, /changelog, /contact, /press,
// /solutions, /design/*. The chrome sits on Iron with Amber accents and
// declines Fraunces in its own body (Fraunces remains available to nested
// specimen content, e.g. /design/letters renders Letters' Fraunces ladder).

export const Route = createFileRoute("/_workshop")({
  component: WorkshopLayout,
});

function WorkshopLayout() {
  return (
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
        <Outlet />
      </main>
      <WorkshopFooter />
    </div>
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

function WorkshopFooter() {
  return (
    <footer
      className="mt-16"
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
          <FooterExternal href="https://verself.sh">Verself Platform</FooterExternal>
        </FooterColumn>
        <FooterColumn heading="Read">
          <FooterLink to="/letters">Letters</FooterLink>
          <FooterExternal href="/letters/rss">RSS</FooterExternal>
          <FooterLink to="/newsroom">Newsroom</FooterLink>
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
