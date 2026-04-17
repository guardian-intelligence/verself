import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";

import { SUBPROCESSORS } from "~/lib/policy-catalog";
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

const getOperatorDomain = createServerFn({ method: "GET" }).handler(() => requireOperatorDomain());

export const Route = createFileRoute("/policy/subprocessors")({
  component: SubprocessorsPage,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Subprocessors — Forge Metal Platform" },
      {
        name: "description",
        content:
          "The third parties Forge Metal engages to operate the service, the data categories they process, and where.",
      },
    ],
  }),
});

function SubprocessorsPage() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Subprocessors" policyId="subprocessors" />
      <Summary />
      <Catalog />
      <ChangeNotification />
      <ChangesSection policyId="subprocessors" />
      <ContactSection operatorDomain={operatorDomain} primary="privacy" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="This page is data-driven">
          — the table below is rendered from the same YAML file that the customer-notification
          system reads, so the list stays canonical.
        </SummaryItem>
        <SummaryItem term={`${SUBPROCESSORS.change_notification.lead_time_days} days' notice`}>
          for additions. Objection rights apply on reasonable data-protection grounds.
        </SummaryItem>
        <SummaryItem term="Subscribe">
          by watching the <a href="/policy/changelog">changelog</a>.
        </SummaryItem>
        <SummaryItem term="DPA linkage">
          each entry below links to the subprocessor's customer DPA.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Catalog() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="catalog">Active subprocessors</SectionHeading>
      <div className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/40 text-left">
              <th scope="col" className="px-4 py-3 font-medium">
                Name
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Purpose
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Data categories
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Processing location
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                DPA
              </th>
            </tr>
          </thead>
          <tbody>
            {SUBPROCESSORS.subprocessors.map((s) => (
              <tr
                key={s.id}
                className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top"
              >
                <td className="font-medium">{s.name}</td>
                <td className="text-muted-foreground">{s.purpose}</td>
                <td className="text-muted-foreground">{s.data_categories.join("; ")}</td>
                <td className="text-muted-foreground">{s.processing_location}</td>
                <td>
                  <a href={s.dpa_url} target="_blank" rel="noreferrer">
                    Link
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function ChangeNotification() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="change-notification">Change notification</SectionHeading>
      <Prose>
        <p>{SUBPROCESSORS.change_notification.channel}</p>
        <p>
          Additions take effect {SUBPROCESSORS.change_notification.lead_time_days} days after
          announcement. The customer may object on reasonable data-protection grounds within the
          notice window. An entry being removed from this page takes effect immediately; a
          subprocessor ceasing to process customer data on our behalf is not a change requiring
          notice.
        </p>
      </Prose>
    </section>
  );
}
