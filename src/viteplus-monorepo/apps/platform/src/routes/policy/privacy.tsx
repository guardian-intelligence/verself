import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";

import { ROPA, SUBPROCESSORS } from "~/lib/policy-catalog";
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

export const Route = createFileRoute("/policy/privacy")({
  component: PrivacyPolicy,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Privacy Policy — Forge Metal Platform" },
      {
        name: "description",
        content:
          "How Forge Metal handles personal data, the roles it plays under data-protection law, and the rights of data subjects.",
      },
    ],
  }),
});

function PrivacyPolicy() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Privacy Policy" policyId="privacy" />
      <Summary />
      <Roles />
      <WhatWeCollect />
      <Ropa />
      <SharingSection />
      <DSRSection />
      <InternationalTransfers />
      <RegionalSupplements />
      <Retention />
      <SecurityPara />
      <Children />
      <ChangesSection policyId="privacy" />
      <ContactSection operatorDomain={operatorDomain} primary="privacy" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="We do not sell personal data.">Full stop.</SummaryItem>
        <SummaryItem term="Controller vs processor.">
          We are <em>controller</em> for account and billing data, <em>processor</em> for the
          workload data your users place on our substrate.
        </SummaryItem>
        <SummaryItem term="Subprocessors">
          are listed at <a href="/policy/subprocessors">/policy/subprocessors</a> with 30-day notice
          of additions.
        </SummaryItem>
        <SummaryItem term="Your rights">
          (access, deletion, correction, portability) are honored — see{" "}
          <a href="#dsr">Data-subject requests</a>.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Roles() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="roles">Roles under data-protection law</SectionHeading>
      <Prose>
        <p>
          Forge Metal plays two distinct roles under GDPR, UK GDPR, the California CPRA, and
          comparable laws:
        </p>
        <ul>
          <li>
            <strong>Controller</strong> for account administration, authentication, billing, and
            operational telemetry.
          </li>
          <li>
            <strong>Processor</strong> for the content your organization places into the substrate —
            durable VM disks, mailboxes, execution logs, and other workload artifacts. We act on
            your documented instructions under the{" "}
            <a href="/policy/dpa">Data Processing Addendum</a>.
          </li>
        </ul>
      </Prose>
    </section>
  );
}

function WhatWeCollect() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="collection">What we collect and why</SectionHeading>
      <Prose>
        <p>
          The categories below are the personal-data categories we hold <em>as controller</em>.
          Workload data you place into the substrate is governed by the DPA, not this section.
        </p>
        <ul>
          <li>
            <strong>Identity and account.</strong> Administrator name and email address,
            organization metadata, authentication events (timestamps, IP addresses, user-agent
            strings).
          </li>
          <li>
            <strong>Billing.</strong> Billing contact, tax identifier, payment instrument metadata
            (held by our payments subprocessor; we see descriptive tokens only), invoice line items,
            usage aggregates.
          </li>
          <li>
            <strong>Support correspondence.</strong> Anything you send to our policy, security, or
            support mailboxes.
          </li>
          <li>
            <strong>Operational telemetry.</strong> Logs, traces, and metrics emitted by the service
            as it runs your workloads. Retained per the{" "}
            <a href="/policy/data-retention#retention">Data Retention policy</a>.
          </li>
        </ul>
      </Prose>
    </section>
  );
}

function Ropa() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="ropa">Record of Processing Activities</SectionHeading>
      <Prose>
        <p>
          Published here to satisfy GDPR Article 30(1) and (2). Each activity identifies the role,
          purpose, data categories, and lawful basis.
        </p>
      </Prose>
      <div className="overflow-x-auto rounded-lg border border-border bg-card">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-secondary/40 text-left">
              <th scope="col" className="px-4 py-3 font-medium">
                Activity
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Role
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Purpose
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Data categories
              </th>
              <th scope="col" className="px-4 py-3 font-medium">
                Lawful basis
              </th>
            </tr>
          </thead>
          <tbody>
            {ROPA.processing_activities.map((a) => (
              <tr
                key={a.id}
                className="border-b border-border last:border-b-0 [&_td]:px-4 [&_td]:py-3 [&_td]:align-top"
              >
                <td className="font-medium">{a.id}</td>
                <td className="capitalize text-muted-foreground">{a.role}</td>
                <td className="text-muted-foreground">{a.purpose}</td>
                <td className="text-muted-foreground">{a.data_categories.join("; ")}</td>
                <td className="text-muted-foreground">{a.lawful_basis}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function SharingSection() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="sharing">Who we share data with</SectionHeading>
      <Prose>
        <p>
          We share personal data only with the subprocessors listed at{" "}
          <a href="/policy/subprocessors">/policy/subprocessors</a>, and only for the purposes
          documented on that page. As of {SUBPROCESSORS.effective_at}, the list is{" "}
          {SUBPROCESSORS.subprocessors.map((s) => s.name).join(", ")}. Additions get{" "}
          {SUBPROCESSORS.change_notification.lead_time_days} days' notice by email to
          administrators.
        </p>
        <p>
          We do not sell personal data and do not share it for targeted advertising, in the sense
          CPRA and similar laws use those terms.
        </p>
      </Prose>
    </section>
  );
}

function DSRSection() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="dsr">Data-subject requests</SectionHeading>
      <Prose>
        <p>
          Under GDPR Articles 15–22, CPRA §1798.100 et seq., VCDPA, CPA, and comparable laws, you
          may request access to personal data about you, correction of inaccuracies, deletion,
          portability, and an objection to processing based on legitimate interests.
        </p>
        <p>
          Where Forge Metal is the <strong>controller</strong> (billing contacts, organization
          administrators, support correspondence), send requests to the privacy mailbox below. We
          respond within 30 days under GDPR and 45 days under CPRA, with extensions only where the
          statute permits.
        </p>
        <p>
          Where Forge Metal is the <strong>processor</strong> (workload data your organization
          processes about its end users), direct requests to the customer. If we receive one
          directly we forward it and cooperate with the response.
        </p>
      </Prose>
    </section>
  );
}

function InternationalTransfers() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="transfers">International transfers</SectionHeading>
      <Prose>
        <p>
          The primary Forge Metal deployment runs on bare-metal hardware in the region chosen by the
          operator. Some subprocessors process data in the United States and other regions as listed
          on <a href="/policy/subprocessors">/policy/subprocessors</a>.
        </p>
        <p>
          Where transfers cross the EEA/UK, the EU Standard Contractual Clauses (Commission
          Implementing Decision (EU) 2021/914) apply via the subprocessor's DPA. The UK
          International Data Transfer Agreement or UK Addendum applies to UK transfers; the Swiss
          addendum applies where Swiss data protection law governs.
        </p>
      </Prose>
    </section>
  );
}

function RegionalSupplements() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="regional">Regional supplements</SectionHeading>
      <Prose>
        <p>
          <strong>California (CPRA).</strong> You have the right to know, delete, correct, and limit
          the use and disclosure of sensitive personal information. We do not sell or share personal
          information in the sense §1798.140(ad) and (ah) use those terms. Authorized agents may
          submit requests with written authorization; we verify the request against the account
          record before responding.
        </p>
        <p>
          <strong>European Economic Area, United Kingdom, Switzerland.</strong> The lawful bases
          above apply. You have the right to lodge a complaint with a supervisory authority in your
          jurisdiction (for example the CNIL, the ICO, or the FDPIC).
        </p>
        <p>
          <strong>
            Other US states (Virginia VCDPA, Colorado CPA, Connecticut CTDPA, Utah UCPA, etc.).
          </strong>{" "}
          Rights under these statutes are honored on the same DSR pipeline.
        </p>
        <p>
          <strong>Brazil (LGPD), Canada (PIPEDA).</strong> Rights under these statutes are honored
          on the same DSR pipeline. Where the statute requires a local representative, the operator
          entity named on your invoice serves that role or designates one in writing.
        </p>
      </Prose>
    </section>
  );
}

function Retention() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="retention">Retention</SectionHeading>
      <Prose>
        <p>
          Personal data is retained according to the{" "}
          <a href="/policy/data-retention">Data Retention policy</a>. Billing records persist for
          the statutory windows listed there; operational telemetry is trimmed by per-table TTL;
          durable workload data follows the account lifecycle.
        </p>
      </Prose>
    </section>
  );
}

function SecurityPara() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="security">Security of processing</SectionHeading>
      <Prose>
        <p>
          We implement the technical and organizational measures described in the{" "}
          <a href="/policy/security">Security Overview</a>. Where we act as processor, the DPA
          supplements those measures with the Article 32 controls the customer is entitled to.
        </p>
      </Prose>
    </section>
  );
}

function Children() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="children">Children</SectionHeading>
      <Prose>
        <p>
          The service is an infrastructure product sold to organizations; we do not knowingly
          collect personal data from children under 16. If you believe a child has provided us with
          personal data, contact the privacy mailbox below and we will delete it.
        </p>
      </Prose>
    </section>
  );
}
