import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireProductDomain } from "@verself/web-env";

import {
  ChangesSection,
  ContactSection,
  PolicyArticle,
  PolicyHeader,
  Prose,
  SectionHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

const getProductDomain = createServerFn({ method: "GET" }).handler(() => requireProductDomain());

export const Route = createFileRoute("/_workshop/policy/cookies")({
  component: CookiesPolicy,
  loader: () => getProductDomain(),
  head: () => ({
    meta: [
      { title: "Cookie Policy — Verself Platform" },
      {
        name: "description",
        content:
          "The cookies Verself sets on customer-facing web surfaces and why each is strictly necessary to deliver the service.",
      },
    ],
  }),
});

function CookiesPolicy() {
  const productDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Cookie Policy" policyId="cookies" />
      <Summary />
      <Inventory />
      <Analytics />
      <Controls />
      <ChangesSection policyId="cookies" />
      <ContactSection productDomain={productDomain} primary="privacy" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Strictly necessary only">
          — every cookie below is required to keep you signed in or to protect the session from
          CSRF. No tracking, advertising, or analytics cookies.
        </SummaryItem>
        <SummaryItem term="No consent banner">
          because we do not set anything that requires one under GDPR or the ePrivacy Directive.
        </SummaryItem>
        <SummaryItem term="First-party only">
          cookies are set by the Verself domain. We do not include third-party trackers.
        </SummaryItem>
        <SummaryItem term="Storage duration">
          is bounded by the session's lifetime; nothing persists beyond sign-out or the expiration
          window described below.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Inventory() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="inventory">Cookie inventory</SectionHeading>
      <div className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/40 text-left">
              <th scope="col" className="px-4 py-3 font-medium">
                Name
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Category
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Purpose
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Lifetime
              </th>
            </tr>
          </thead>
          <tbody>
            <tr className="border-b border-border [&_td]:px-4 [&_td]:py-3 [&_td]:align-top">
              <td className="font-mono text-xs">verself_session</td>
              <td className="text-muted-foreground">Strictly necessary</td>
              <td className="text-muted-foreground">
                HTTP-only, SameSite=Lax session cookie backing the server-owned single sign-on
                session. Its value is an opaque server identifier that resolves to the session
                record in our server-side session store.
              </td>
              <td className="text-muted-foreground">Session; rotated on sign-in</td>
            </tr>
            <tr className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top">
              <td className="font-mono text-xs">verself_csrf</td>
              <td className="text-muted-foreground">Strictly necessary</td>
              <td className="text-muted-foreground">
                CSRF token paired with the session cookie to authorize state-changing server
                functions. Rotated on every request.
              </td>
              <td className="text-muted-foreground">Session</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  );
}

function Analytics() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="analytics">Analytics, advertising, social</SectionHeading>
      <Prose>
        <p>
          We do not set analytics, advertising, retargeting, or social-widget cookies on customer
          surfaces. Product analytics — where we measure them — run server-side against request
          traces in our observability store and do not require browser-side cookies to be useful.
        </p>
      </Prose>
    </section>
  );
}

function Controls() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="controls">Controls</SectionHeading>
      <Prose>
        <p>
          Because we set only strictly necessary cookies, there is no consent banner to dismiss. You
          can clear Verself cookies from your browser at any time; doing so will end the session and
          require re-authentication on the next request.
        </p>
      </Prose>
    </section>
  );
}
