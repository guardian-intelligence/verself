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

export const Route = createFileRoute("/policy/sla")({
  component: SLA,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Service Level Agreement — Forge Metal Platform" },
      {
        name: "description",
        content:
          "Availability commitments and service credits for the Forge Metal platform, and the deployment topology on which they apply.",
      },
    ],
  }),
});

function SLA() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Service Level Agreement" policyId="sla" />
      <Summary />
      <CurrentTier />
      <Maintenance />
      <Support />
      <FutureTier />
      <ChangesSection policyId="sla" />
      <ContactSection operatorDomain={operatorDomain} primary="policy" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Current tier">
          <em>No availability SLA.</em> The single-node deployment does not carry an availability
          commitment; we aim to operate it reliably but do not promise credits for outages.
        </SummaryItem>
        <SummaryItem term="Operator transparency">
          We publish incident notices on the changelog and in the organization audit trail.
        </SummaryItem>
        <SummaryItem term="Future tier">
          A three-node topology introduces replication across every stateful component and, with it,
          a 99.9% SLA with service credits.
        </SummaryItem>
        <SummaryItem term="Support">
          Response and resolution targets are scoped to the contracted support plan.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function CurrentTier() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="current-tier">Current tier: single-node</SectionHeading>
      <Prose>
        <p>
          Forge Metal is currently deployed on a single bare-metal node. Every component — the
          orchestrator, databases, auth, billing, ingress — runs on that node and is not replicated
          across failure domains. A hardware failure, a kernel crash, or a datacenter-level event is
          therefore a correlated outage of the whole service.
        </p>
        <p>
          We do not offer a guaranteed availability percentage or service credits on this tier. This
          is not a gap in the product; it is a property of single-node deployment, and pricing
          reflects it. Customers whose workload needs an availability SLA should wait for the
          three-node tier described below or bring their own redundancy.
        </p>
        <p>
          The operator monitors the node with the same observability stack described in the{" "}
          <a href="/policy/security#logging">Security Overview</a>. Material incidents are posted on
          the <a href="/policy/changelog">changelog</a> and mirrored into each affected
          organization's audit trail.
        </p>
      </Prose>
    </section>
  );
}

function Maintenance() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="maintenance">Planned maintenance</SectionHeading>
      <Prose>
        <p>
          Planned maintenance that may affect availability is announced at least 48 hours in advance
          to account administrators and is performed in a published maintenance window. Emergency
          maintenance (security patches, active-incident response) is performed without prior
          notice; a post-incident writeup is published on the changelog.
        </p>
      </Prose>
    </section>
  );
}

function Support() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="support">Support</SectionHeading>
      <Prose>
        <p>
          Support response and resolution targets are defined in the support plan named on your
          order form. In the absence of a specified plan, support operates on best-effort,
          business-hour basis through the <a href="#contact">policy mailbox</a>.
        </p>
        <p>
          Security reports take precedence over routine support correspondence; they route through{" "}
          the security mailbox and are acknowledged on receipt. See{" "}
          <a href="/policy/security#disclosure">Coordinated disclosure</a>.
        </p>
      </Prose>
    </section>
  );
}

function FutureTier() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="future-tier">Three-node tier (roadmap)</SectionHeading>
      <Prose>
        <p>
          The next topology introduces three nodes, cross-node replication for every stateful
          component (TigerBeetle consensus, ClickHouse ReplicatedMergeTree, Postgres streaming
          replication, ZFS send-based durable data replication), and a Netbird overlay for
          inter-node traffic. With this topology comes a 99.9%-per-calendar-month availability
          commitment, measured as the percentage of minutes in which each customer-facing service
          endpoint responds with a non-5xx status to a synthetic probe.
        </p>
        <p>
          Downtime below 99.9% triggers service credits on a sliding scale against the affected
          month's subscription fees. The exact credit schedule, exclusions (force majeure,
          customer-caused outages, dependencies on third-party systems), and claim procedure will be
          published alongside the three-node tier's general availability.
        </p>
      </Prose>
    </section>
  );
}
