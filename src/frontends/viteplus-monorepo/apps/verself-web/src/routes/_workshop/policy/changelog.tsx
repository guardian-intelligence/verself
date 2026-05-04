import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireProductDomain } from "@verself/web-env";

import { VERSIONS, formatPrettyDate } from "~/lib/policy-catalog";
import {
  ContactSection,
  PolicyArticle,
  PolicyHeader,
  SectionHeading,
  SummaryItem,
  SummaryPanel,
} from "~/features/policy/policy-primitives";

const getProductDomain = createServerFn({ method: "GET" }).handler(() => requireProductDomain());

export const Route = createFileRoute("/_workshop/policy/changelog")({
  component: PolicyChangelog,
  loader: () => getProductDomain(),
  head: () => ({
    meta: [
      { title: "Policy Changelog — Verself Platform" },
      {
        name: "description",
        content:
          "Every policy change Verself has announced, in the order it took effect, rendered from the canonical versions.yml source.",
      },
    ],
  }),
});

function PolicyChangelog() {
  const productDomain = Route.useLoaderData();
  const sorted = [...VERSIONS.entries].sort((a, b) => (a.date < b.date ? 1 : -1));
  return (
    <PolicyArticle>
      <PolicyHeader title="Policy Changelog" />
      <Summary />
      <section className="flex flex-col gap-4">
        <SectionHeading id="entries">Entries</SectionHeading>
        <ol className="flex flex-col gap-4">
          {sorted.map((entry) => (
            <li
              key={entry.version}
              id={entry.version}
              className="flex flex-col gap-2 rounded-lg border border-border bg-card p-5 scroll-mt-[var(--header-scroll-offset)]"
            >
              <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-sm">
                <span className="font-mono text-xs tabular-nums text-muted-foreground">
                  {entry.version}
                </span>
                <span className="font-medium">{formatPrettyDate(entry.date)}</span>
              </div>
              <p className="text-sm leading-6">{entry.summary}</p>
              <ul className="flex flex-wrap gap-1 text-xs">
                {entry.policies.map((policy) => (
                  <li
                    key={policy}
                    className="rounded-md border border-border bg-secondary/40 px-2 py-0.5 font-mono tabular-nums text-muted-foreground"
                  >
                    {policy}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ol>
      </section>
      <ContactSection productDomain={productDomain} primary="policy" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Source of truth">
          is <code>src/frontends/viteplus-monorepo/apps/verself-web/src/policies/versions.yml</code>
          ; this page is rendered from it.
        </SummaryItem>
        <SummaryItem term="Commit-addressable">
          — every entry corresponds to a git commit; diff the YAML file in the monorepo to see
          exactly what changed.
        </SummaryItem>
        <SummaryItem term="Material changes">
          take effect 30 days after the announcement for the policies they list.
        </SummaryItem>
        <SummaryItem term="Subscription">
          is delivered via the per-organization administrator-email notification; feed subscription
          lands with the next revision.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}
