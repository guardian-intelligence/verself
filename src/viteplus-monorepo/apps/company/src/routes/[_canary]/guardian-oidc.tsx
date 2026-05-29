import { createFileRoute, Link } from "@tanstack/react-router";
import { AppChrome } from "@verself/brand";
import { ArrowRight, KeyRound, ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";
import { TopNav } from "~/components/top-nav";
import {
  GUARDIAN_OIDC_CANARY_CALLBACK_PATH,
  GUARDIAN_OIDC_CANARY_ISSUER,
  GUARDIAN_OIDC_CANARY_START_PATH,
  parseGuardianOidcCanaryStatus,
  type GuardianOidcCanaryStatus,
} from "~/features/guardian-oidc-canary/constants";
import { criticalTreatmentHead, criticalTreatmentRootStyle } from "~/lib/critical-treatment";
import { ogMeta } from "~/lib/head";
import workshopCriticalCss from "~/styles/critical/workshop.css?inline";

export const Route = createFileRoute("/_canary/guardian-oidc")({
  validateSearch: (search: Record<string, unknown>) => ({
    status: parseGuardianOidcCanaryStatus(search.status),
    reason: typeof search.reason === "string" ? search.reason.slice(0, 80) : undefined,
  }),
  component: GuardianOIDCCanary,
  head: guardianHead,
});

function guardianHead() {
  const critical = criticalTreatmentHead("workshop", workshopCriticalCss);
  return {
    meta: [
      ...critical.meta,
      ...ogMeta({
        slug: "home",
        title: "Continue with Guardian canary",
        description:
          "A public Guardian Intelligence relying-party canary for exercising the Verself OIDC issuer.",
      }),
    ],
    styles: critical.styles,
  };
}

function GuardianOIDCCanary() {
  const { status, reason } = Route.useSearch();
  const statusCopy = status ? guardianStatusCopy(status, reason) : undefined;

  return (
    <div
      data-treatment="workshop"
      className="flex min-h-svh flex-col"
      style={criticalTreatmentRootStyle("workshop")}
    >
      <AppChrome treatment="workshop" LinkComponent={LinkAdapter} slotRight={<TopNav />} />
      <main id="main" className="flex-1">
        <section className="min-h-[calc(100svh-var(--header-h))] bg-[var(--treatment-ground)] text-[var(--treatment-ink)]">
          <div className="mx-auto grid w-full max-w-6xl gap-10 px-4 py-12 md:grid-cols-[minmax(0,1fr)_360px] md:px-6 md:py-16">
            <div className="flex max-w-2xl flex-col justify-center">
              <p className="font-mono text-[11px] font-medium uppercase tracking-[0.18em] text-[var(--treatment-muted-faint)]">
                Guardian Intelligence relying-party canary
              </p>
              <h1 className="mt-5 text-4xl font-semibold leading-tight md:text-5xl">
                Continue with Guardian
              </h1>
              <p className="mt-5 max-w-xl text-base leading-7 text-[var(--treatment-muted)]">
                This page is a public test surface for a Guardian Intelligence account using the
                Verself OIDC issuer. Continuing opens Verself authentication and returns only an
                authorization response status here until token exchange is wired through IAM
                service.
              </p>

              <form method="get" action={GUARDIAN_OIDC_CANARY_START_PATH} className="mt-8">
                <button
                  type="submit"
                  className="inline-flex min-h-12 items-center gap-3 rounded-md border border-[var(--treatment-ink)] bg-[var(--treatment-ink)] px-5 text-sm font-semibold text-[var(--treatment-ground)] shadow-sm transition hover:translate-y-[-1px] focus:outline-none focus:ring-2 focus:ring-[var(--color-amber)] focus:ring-offset-2 focus:ring-offset-[var(--treatment-ground)]"
                >
                  <ShieldCheck aria-hidden="true" size={18} strokeWidth={2} />
                  <span>Continue with Guardian</span>
                  <ArrowRight aria-hidden="true" size={16} strokeWidth={2} />
                </button>
              </form>

              {statusCopy ? (
                <div
                  className="mt-8 max-w-xl rounded-md border px-4 py-3 text-sm leading-6"
                  style={{
                    borderColor: statusCopy.border,
                    background: statusCopy.background,
                    color: statusCopy.ink,
                  }}
                >
                  <p className="font-semibold">{statusCopy.title}</p>
                  <p className="mt-1">{statusCopy.body}</p>
                </div>
              ) : null}
            </div>

            <aside className="self-start rounded-md border border-[var(--treatment-rule-color)] bg-[color-mix(in_srgb,var(--treatment-ground)_88%,var(--treatment-ink)_12%)] p-5">
              <div className="flex items-center gap-3">
                <div className="flex size-10 items-center justify-center rounded-md bg-[var(--treatment-ink)] text-[var(--treatment-ground)]">
                  <KeyRound aria-hidden="true" size={18} strokeWidth={2} />
                </div>
                <div>
                  <p className="text-sm font-semibold">OIDC contract</p>
                  <p className="font-mono text-[11px] text-[var(--treatment-muted-faint)]">
                    internal canary
                  </p>
                </div>
              </div>
              <dl className="mt-5 grid gap-4 text-sm">
                <CanaryDatum label="Issuer" value={GUARDIAN_OIDC_CANARY_ISSUER} />
                <CanaryDatum
                  label="Redirect URI"
                  value={`https://guardianintelligence.org${GUARDIAN_OIDC_CANARY_CALLBACK_PATH}`}
                />
                <CanaryDatum label="Response type" value="code + PKCE S256" />
                <CanaryDatum label="Scopes" value="openid profile email" />
              </dl>
            </aside>
          </div>
        </section>
      </main>
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

function CanaryDatum({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="font-mono text-[10px] font-medium uppercase tracking-[0.16em] text-[var(--treatment-muted-faint)]">
        {label}
      </dt>
      <dd className="mt-1 break-words font-mono text-[12px] leading-5 text-[var(--treatment-muted)]">
        {value}
      </dd>
    </div>
  );
}

function guardianStatusCopy(status: GuardianOidcCanaryStatus, reason: string | undefined) {
  switch (status) {
    case "authorization_response":
      return {
        title: "Authorization response received.",
        body: "Verself redirected back with a code and matching state. Token exchange is intentionally not part of this canary route yet.",
        border: "color-mix(in srgb, var(--color-amber) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-amber) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
    case "missing_client_registration":
      return {
        title: "Guardian OIDC client registration is missing.",
        body: "Set GUARDIAN_OIDC_CLIENT_ID on the company app deployment before this relying-party canary can redirect to Verself.",
        border: "color-mix(in srgb, var(--color-amber) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-amber) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
    case "provider_error":
      return {
        title: "Verself returned an authorization error.",
        body: reason
          ? `Provider error: ${reason}.`
          : "The provider returned an error without a usable reason.",
        border: "color-mix(in srgb, var(--color-redwood) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-redwood) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
    case "state_mismatch":
      return {
        title: "State did not match.",
        body: "The authorization callback did not match the canary cookie. Start the flow again from this page.",
        border: "color-mix(in srgb, var(--color-redwood) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-redwood) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
    case "invalid_canary_cookie":
      return {
        title: "Canary cookie was missing or expired.",
        body: "The callback arrived without the short-lived HttpOnly state cookie. Start the flow again from this page.",
        border: "color-mix(in srgb, var(--color-redwood) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-redwood) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
    case "missing_response":
      return {
        title: "Callback was missing OIDC response fields.",
        body: "The callback did not include either an authorization code or a provider error.",
        border: "color-mix(in srgb, var(--color-redwood) 70%, transparent)",
        background: "color-mix(in srgb, var(--color-redwood) 12%, transparent)",
        ink: "var(--treatment-ink)",
      };
  }
}
