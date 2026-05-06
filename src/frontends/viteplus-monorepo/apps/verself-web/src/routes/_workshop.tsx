import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { type CSSProperties, type ReactNode } from "react";
import { PublicTopBar } from "~/features/public/public-top-bar";

// Public workshop surface for /docs and /policy. The signed-in console shell
// uses its own sidebar chrome — see _shell/route.tsx.
export const Route = createFileRoute("/_workshop")({
  component: WorkshopLayout,
});

function WorkshopLayout() {
  return (
    <div
      data-treatment="workshop"
      className="flex min-h-svh flex-col"
      style={
        {
          "--header-h": "4rem",
          "--header-scroll-offset": "calc(var(--header-h) + 1.5rem)",
          background: "var(--treatment-ground)",
          color: "var(--treatment-ink)",
        } as CSSProperties
      }
    >
      <PublicTopBar />
      <main id="main" className="flex-1">
        <Outlet />
      </main>
      <SiteFooter />
    </div>
  );
}

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
