import { createFileRoute } from "@tanstack/react-router";
import { createServerFn } from "@tanstack/react-start";
import { requireProductDomain } from "@verself/web-env";

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

const getProductDomain = createServerFn({ method: "GET" }).handler(() => requireProductDomain());

export const Route = createFileRoute("/policy/dpa")({
  component: DPA,
  loader: () => getProductDomain(),
  head: () => ({
    meta: [
      { title: "Data Processing Addendum — Verself Platform" },
      {
        name: "description",
        content:
          "Processor obligations, subprocessor commitments, and international transfer mechanics for Verself customer data.",
      },
    ],
  }),
});

function DPA() {
  const productDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Data Processing Addendum" policyId="dpa" />
      <Summary />
      <Scope />
      <Instructions />
      <Confidentiality />
      <SecuritySection />
      <Subprocessing />
      <Transfers />
      <DSRAssistance />
      <Incident />
      <ReturnDeletion />
      <Audit />
      <ChangesSection policyId="dpa" />
      <ContactSection productDomain={productDomain} primary="dpo" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Role">
          Verself is a <em>processor</em> for the workload data your users place into the substrate.
        </SummaryItem>
        <SummaryItem term="SCCs">
          EU SCCs (Commission Decision 2021/914) apply for transfers out of the EEA; the UK IDTA and
          Swiss addendum apply for those jurisdictions.
        </SummaryItem>
        <SummaryItem term="Subprocessors">
          listed at <a href="/policy/subprocessors">/policy/subprocessors</a> with 30-day prior
          notice of additions.
        </SummaryItem>
        <SummaryItem term="Incident notice">
          within 72 hours of a qualifying personal-data breach, aligned with GDPR Art. 33(2).
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Scope() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="scope">Scope and definitions</SectionHeading>
      <Prose>
        <p>
          This Addendum governs Verself's processing of Personal Data on behalf of the customer, in
          the context of the Terms of Service and any applicable order form. Terms not defined here
          have the meaning given in the Terms or in GDPR Art. 4.
        </p>
        <p>
          "Customer Personal Data" means Personal Data contained in durable customer data,
          operational data, or other workload artifacts processed by Verself on the customer's
          documented instructions. "Subprocessor" has the meaning in GDPR Art. 28(4) and includes
          each entity listed on <a href="/policy/subprocessors">the subprocessor page</a>.
        </p>
        <p>
          This Addendum applies to processing subject to the GDPR, UK GDPR, the Swiss FADP, and — as
          to the rights it confers on data subjects — the California CPRA and comparable U.S. state
          privacy laws.
        </p>
      </Prose>
    </section>
  );
}

function Instructions() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="instructions">Documented instructions</SectionHeading>
      <Prose>
        <p>
          Verself processes Customer Personal Data only on the customer's documented instructions,
          which are the Terms of Service, this Addendum, any order form, and the customer's use of
          the service's configured functionality (running workloads, configuring mailboxes, calling
          APIs). We will inform the customer if, in our opinion, a specific instruction infringes
          applicable data-protection law.
        </p>
      </Prose>
    </section>
  );
}

function Confidentiality() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="confidentiality">Personnel confidentiality</SectionHeading>
      <Prose>
        <p>
          Personnel authorized by Verself to process Customer Personal Data are bound by written
          confidentiality obligations that survive termination of their engagement.
        </p>
      </Prose>
    </section>
  );
}

function SecuritySection() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="security">Security of processing (Art. 32)</SectionHeading>
      <Prose>
        <p>
          Verself implements the technical and organizational measures described in the{" "}
          <a href="/policy/security">Security Overview</a>, which are designed to ensure a level of
          security appropriate to the risk. Those measures include:
        </p>
        <ul>
          <li>Hardware-virtualized microVM isolation of tenant workloads.</li>
          <li>Per-tenant durable-storage separation, with encryption at rest where configured.</li>
          <li>
            TLS 1.3 for all inter-service and customer-facing traffic; short-lived bearer tokens
            validated against the identity provider's published key set.
          </li>
          <li>
            Defense-in-depth via a host firewall, an inline web application firewall at the edge
            reverse proxy, and per-service credential isolation.
          </li>
          <li>Comprehensive audit logging and distributed-tracing observability.</li>
        </ul>
        <p>
          The customer is responsible for ensuring the availability of Customer Personal Data by
          maintaining their own backups; Verself does not currently provide a backup product, per
          the <a href="/policy/data-retention">Data Retention policy</a>.
        </p>
      </Prose>
    </section>
  );
}

function Subprocessing() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="subprocessing">Subprocessors</SectionHeading>
      <Prose>
        <p>
          The customer grants Verself general authorization to engage the subprocessors listed at{" "}
          <a href="/policy/subprocessors">/policy/subprocessors</a>. Each subprocessor is bound by a
          written agreement imposing data-protection obligations materially no less protective than
          those set out here.
        </p>
        <p>
          Additions to the subprocessor list are announced with{" "}
          {SUBPROCESSORS.change_notification.lead_time_days} days' prior notice to administrators.
          The customer may object to an addition within that window on reasonable data-protection
          grounds; if the parties cannot agree on a commercially reasonable alternative, either
          party may terminate the affected service.
        </p>
      </Prose>
    </section>
  );
}

function Transfers() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="transfers">International transfers</SectionHeading>
      <Prose>
        <p>
          Where processing involves the transfer of Customer Personal Data out of the EEA,
          Switzerland, or the UK to a jurisdiction that is not the subject of an adequacy decision,
          the Standard Contractual Clauses in Commission Implementing Decision (EU) 2021/914 ("EU
          SCCs") apply and are hereby incorporated, with Verself as importer and the customer as
          exporter:
        </p>
        <ul>
          <li>Module Two (controller to processor) where the customer is a controller.</li>
          <li>Module Three (processor to processor) where the customer is itself a processor.</li>
        </ul>
        <p>
          For transfers out of the United Kingdom, the UK International Data Transfer Addendum (as
          issued by the ICO under Section 119A of the Data Protection Act 2018) applies. For
          transfers out of Switzerland, the Swiss FDPIC's SCC adaptation applies. The parties select
          the supervisory authority, governing law, and courts specified in Annex I.C and Clause 17
          as the jurisdiction of the Verself entity unless an order form specifies otherwise.
        </p>
      </Prose>
    </section>
  );
}

function DSRAssistance() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="dsr-assistance">Assistance with data-subject requests</SectionHeading>
      <Prose>
        <p>
          Taking into account the nature of the processing and the information available, Forge
          Verself provides reasonable assistance to the customer in fulfilling obligations under
          GDPR Articles 15–22. This assistance is built into the product: export functionality,
          deletion on request, audit trail access. Bespoke extraction beyond that functionality may
          be charged at cost.
        </p>
      </Prose>
    </section>
  );
}

function Incident() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="incident">Personal-data breach notification</SectionHeading>
      <Prose>
        <p>
          Verself notifies the customer without undue delay and in any event within 72 hours of
          becoming aware of a Personal Data Breach affecting Customer Personal Data, in line with
          GDPR Art. 33(2). The notice will describe the nature of the breach, categories and
          approximate numbers of data subjects affected, likely consequences, and the measures taken
          or proposed to address it.
        </p>
      </Prose>
    </section>
  );
}

function ReturnDeletion() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="return-deletion">Return and deletion on termination</SectionHeading>
      <Prose>
        <p>
          On termination of the underlying service, Customer Personal Data is handled per the{" "}
          <a href="/policy/data-retention">Data Retention policy</a>: export is available for 30
          days; durable data is deleted 90 days after closure; billing records are retained for the
          statutory windows. Backups held by upstream subprocessors are cycled out in line with
          those subprocessors' DPAs.
        </p>
      </Prose>
    </section>
  );
}

function Audit() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="audit">Audit and information rights</SectionHeading>
      <Prose>
        <p>
          Verself makes available to the customer the information necessary to demonstrate
          compliance with GDPR Art. 28 and allows for and contributes to audits, including
          inspections, conducted by the customer or an auditor mandated by the customer. The primary
          method is the public documentation and audit artifacts we publish — including the Security
          Overview, the Record of Processing Activities, this Addendum, and any SOC 2 or ISO 27001
          attestations we hold. On written request, we will provide additional reasonable
          information needed to meet an audit obligation.
        </p>
      </Prose>
    </section>
  );
}
