import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireOperatorDomain } from "@forge-metal/web-env";

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

export const Route = createFileRoute("/policy/terms")({
  component: TermsPolicy,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Terms of Service — Forge Metal Platform" },
      {
        name: "description",
        content:
          "The contract under which customer organizations use the Forge Metal platform, and its relationship to the other policy documents.",
      },
    ],
  }),
});

function TermsPolicy() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Terms of Service" policyId="terms" />
      <Summary />
      <Agreement />
      <Accounts />
      <Use />
      <Fees />
      <Content />
      <Services />
      <Warranty />
      <Liability />
      <Termination />
      <LawAndDisputes />
      <ChangesSection policyId="terms" />
      <ContactSection operatorDomain={operatorDomain} primary="legal" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Contracting party">
          The Forge Metal operator entity identified on your invoice.
        </SummaryItem>
        <SummaryItem term="Agreement stack">
          These Terms, the <a href="/policy/acceptable-use">AUP</a>, the{" "}
          <a href="/policy/dpa">DPA</a>, the <a href="/policy/sla">SLA</a>, the{" "}
          <a href="/policy/privacy">Privacy Policy</a>, and any signed order form. Order-form terms
          control where they conflict with these.
        </SummaryItem>
        <SummaryItem term="Data handling">
          Governed by the <a href="/policy/data-retention">Data Retention</a> policy and DPA. You
          remain controller of your end-user data.
        </SummaryItem>
        <SummaryItem term="Termination">
          Either side may terminate for material breach with a reasonable cure period. Retention,
          export, and billing close-out follow the Data Retention policy.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Agreement() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="agreement">Agreement</SectionHeading>
      <Prose>
        <p>
          These Terms of Service form a binding agreement between the customer organization
          identified on the account (<strong>you</strong>) and the Forge Metal operator entity
          identified on your invoice (<strong>we</strong>, <strong>us</strong>,{" "}
          <strong>Forge Metal</strong>). By creating an account, placing an order, or using the
          service you accept these Terms on behalf of the organization and confirm you have
          authority to bind it.
        </p>
        <p>
          These Terms incorporate the <a href="/policy/acceptable-use">Acceptable Use Policy</a>,
          the <a href="/policy/dpa">Data Processing Addendum</a>, the{" "}
          <a href="/policy/sla">Service Level Agreement</a>, the{" "}
          <a href="/policy/privacy">Privacy Policy</a>, the{" "}
          <a href="/policy/data-retention">Data Retention policy</a>, and the{" "}
          <a href="/policy/subprocessors">Subprocessor List</a>. A signed order form between the
          parties controls where its terms expressly conflict with these.
        </p>
      </Prose>
    </section>
  );
}

function Accounts() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="accounts">Accounts and access</SectionHeading>
      <Prose>
        <p>
          You are responsible for the administrators you designate on your account and for every
          action taken under credentials you issue. You agree to keep your organization
          administrator roster current, to require multi-factor authentication for every
          administrator, and to promptly revoke access for users who no longer need it.
        </p>
        <p>
          We log authentication events and administrative actions in an audit trail you can read on
          your billing page. That audit trail is retained per the{" "}
          <a href="/policy/data-retention#retention">Data Retention policy</a>.
        </p>
      </Prose>
    </section>
  );
}

function Use() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="use">Your use of the service</SectionHeading>
      <Prose>
        <p>
          You may use the service only in accordance with these Terms, the{" "}
          <a href="/policy/acceptable-use">Acceptable Use Policy</a>, and applicable law. You are
          responsible for the workloads you run on Forge Metal, for the data you place into its
          substrate, and for the consequences of both.
        </p>
        <p>
          You grant us the limited rights necessary to operate the service on your behalf —
          scheduling virtual machines, storing durable volumes, delivering email on your subdomains,
          issuing invoices — and no broader license over your data.
        </p>
      </Prose>
    </section>
  );
}

function Fees() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="fees">Fees, invoicing, and taxes</SectionHeading>
      <Prose>
        <p>
          Fees are the subscription charges, metered usage charges, and one-time items set out on
          your current plan or order form. Metered usage is billed on the schedule published on the
          product's pricing page; subscription charges are billed in advance for the plan period.
        </p>
        <p>
          Invoices are due on the terms shown on the invoice. Late payment may cause the account to
          transition through <a href="/policy/data-retention#lifecycle">past due and suspended</a>.
          Prices exclude taxes; you are responsible for any taxes other than those on our net
          income.
        </p>
      </Prose>
    </section>
  );
}

function Content() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="content">Your content</SectionHeading>
      <Prose>
        <p>
          You retain all right, title, and interest in the data your organization places into the
          service, including durable volumes, mailboxes, and workload outputs. We claim no ownership
          over that content and do not use it to train models or to enrich third-party data
          products.
        </p>
        <p>
          You are responsible for having the legal right to place that content onto the service, for
          honoring data-subject requests under applicable privacy law (see the{" "}
          <a href="/policy/data-retention#dsr">Data Subject Rights section</a>), and for complying
          with the <a href="/policy/acceptable-use">Acceptable Use Policy</a>.
        </p>
      </Prose>
    </section>
  );
}

function Services() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="services">Service changes and availability</SectionHeading>
      <Prose>
        <p>
          We may add, change, or remove features of the service at any time. Breaking changes to
          documented APIs will be announced with at least 30 days' notice to account administrators.
          Availability targets, where they exist, are set out in the{" "}
          <a href="/policy/sla">Service Level Agreement</a>.
        </p>
      </Prose>
    </section>
  );
}

function Warranty() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="warranty">Warranty and disclaimers</SectionHeading>
      <Prose>
        <p>
          We warrant that we will provide the service with reasonable skill and care and in
          compliance with applicable law. Except for that warranty, the service is provided on an
          "as is" and "as available" basis to the fullest extent permitted by law. We do not warrant
          that the service will be uninterrupted, error-free, or that it will meet every requirement
          you have not communicated to us in writing.
        </p>
      </Prose>
    </section>
  );
}

function Liability() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="liability">Limitation of liability</SectionHeading>
      <Prose>
        <p>
          To the maximum extent permitted by law, neither party will be liable for indirect,
          consequential, incidental, or special damages arising under these Terms. Each party's
          aggregate liability under or in connection with these Terms is capped at the fees paid by
          the customer to Forge Metal in the twelve months preceding the event giving rise to the
          claim.
        </p>
        <p>
          The cap does not apply to a party's indemnification obligations, breach of
          confidentiality, breach of the DPA by the processor, or any liability that cannot be
          limited under applicable law.
        </p>
      </Prose>
    </section>
  );
}

function Termination() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="termination">Termination</SectionHeading>
      <Prose>
        <p>
          Either party may terminate these Terms for material breach by the other that is not cured
          within 30 days of written notice. You may terminate at any time by closing your account in
          the console; we may terminate immediately in the event of non-payment under the lifecycle
          described in the <a href="/policy/data-retention#lifecycle">Data Retention policy</a>, or
          a serious Acceptable Use Policy violation.
        </p>
        <p>
          On termination, the account enters the lifecycle states set out in the Data Retention
          policy and your data is handled per its retention, export, and deletion windows. Billing
          records persist for statutory retention.
        </p>
      </Prose>
    </section>
  );
}

function LawAndDisputes() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="law">Governing law and disputes</SectionHeading>
      <Prose>
        <p>
          These Terms are governed by the laws of the jurisdiction in which the Forge Metal operator
          entity is incorporated, excluding its conflict-of-laws provisions. Each party irrevocably
          submits to the exclusive jurisdiction of the competent courts of that jurisdiction for any
          dispute arising under these Terms.
        </p>
        <p>
          Where an order form specifies a different governing law or disputes forum, that order form
          controls.
        </p>
      </Prose>
    </section>
  );
}
