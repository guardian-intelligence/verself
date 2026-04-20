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

export const Route = createFileRoute("/policy/security")({
  component: SecurityOverview,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Security Overview — Forge Metal Platform" },
      {
        name: "description",
        content:
          "The technical and organizational measures Forge Metal implements to secure customer data and workloads.",
      },
    ],
  }),
});

function SecurityOverview() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Security Overview" policyId="security" />
      <Summary />
      <Identity />
      <Isolation />
      <Encryption />
      <Network />
      <Logging />
      <Personnel />
      <Disclosure />
      <ChangesSection policyId="security" />
      <ContactSection operatorDomain={operatorDomain} primary="security" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Workload isolation">
          Hardware-virtualized microVMs with per-tenant durable-storage separation.
        </SummaryItem>
        <SummaryItem term="Identity">
          Single sign-on with short-lived bearer tokens; every service is its own security boundary.
        </SummaryItem>
        <SummaryItem term="Transport">
          TLS 1.3 everywhere; first-party reverse proxy with an inline web application firewall.
        </SummaryItem>
        <SummaryItem term="Disclosure">
          Coordinated via the security mailbox; safe-harbor for good-faith research.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Identity() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="identity">Identity and access</SectionHeading>
      <Prose>
        <p>
          One identity provider is the authority for authentication and organization membership.
          Every service validates bearer tokens against the provider's published key set using
          cached keys and local cryptography; identity (subject, organization, roles) is extracted
          from token claims and attached to the request context. No long-lived service credentials
          are issued; short-lived bearer tokens replace them.
        </p>
        <p>
          Organization administrators can require multi-factor authentication and passkeys through
          the identity console. Session configuration — password rules, MFA enforcement, SSO
          federation — is customer-editable end to end.
        </p>
      </Prose>
    </section>
  );
}

function Isolation() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="isolation">Workload isolation</SectionHeading>
      <Prose>
        <p>
          Customer workloads run inside hardware-virtualized microVMs, with per-tenant durable
          volumes as their filesystem. VM-to-VM isolation is a hypervisor-level boundary; host
          access is restricted to a single privileged orchestration daemon. Product services have no
          host-level access to the hypervisor device nodes, storage administration interfaces, or
          jail directories — they interact with the compute substrate only through a narrow,
          policy-checked API.
        </p>
        <p>
          In-guest telemetry is collected over an isolated host-to-guest channel; the guest does not
          reach the host network by default.
        </p>
      </Prose>
    </section>
  );
}

function Encryption() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="encryption">Encryption and key management</SectionHeading>
      <Prose>
        <p>
          All external and inter-service traffic uses TLS 1.3, terminated by a first-party reverse
          proxy running a web application firewall; certificates are issued automatically using
          DNS-01 challenges. Operational secrets are held encrypted at rest in the platform
          inventory and delivered to each service through an ephemeral credential loader, so
          application processes read them from process-private memory rather than from files on
          disk.
        </p>
        <p>
          Dataset-level encryption at rest is supported for durable customer volumes; the encryption
          posture of each volume is visible on its billing record.
        </p>
      </Prose>
    </section>
  );
}

function Network() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="network">Network posture</SectionHeading>
      <Prose>
        <p>
          The host firewall denies by default and allows inbound traffic on a per-service basis;
          services listen only where their declared role requires. Single-node deployments keep
          inter-service traffic on loopback; multi-node deployments route inter-service traffic over
          a private overlay network with the same allowlist model. Customer-facing ingress is
          TLS-terminated at the reverse proxy before reaching any product service.
        </p>
      </Prose>
    </section>
  );
}

function Logging() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="logging">Logging and detection</SectionHeading>
      <Prose>
        <p>
          Every service emits structured logs, traces, and metrics into a central observability
          store. Administrative actions taken on the host — including deploys — emit spans
          correlated to the same trace identifier as the service calls they trigger, so
          post-incident inspection reads as a single narrative.
        </p>
        <p>
          Detection thresholds and alert routes run on the founder dashboard. Security-incident
          artifacts are retained separately from normal operational TTLs — see the{" "}
          <a href="/policy/data-retention#incident">Data Retention policy</a>.
        </p>
      </Prose>
    </section>
  );
}

function Personnel() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="personnel">Personnel and access</SectionHeading>
      <Prose>
        <p>
          Access to the host and to production data is restricted to named founders, authenticating
          with hardware security keys. Administrative actions taken on the host are audit-logged and
          fed into the same observability pipeline as service traces.
        </p>
      </Prose>
    </section>
  );
}

function Disclosure() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="disclosure">Coordinated disclosure</SectionHeading>
      <Prose>
        <p>
          We welcome security research. Report findings to the security mailbox below, or via our
          published <code>/.well-known/security.txt</code>. Good-faith research conducted in line
          with the{" "}
          <a href="https://disclose.io/terms" rel="noreferrer">
            disclose.io
          </a>{" "}
          core terms is covered by a safe-harbor commitment: we will not pursue legal action, and
          will work with you on coordinated disclosure. Do not exfiltrate customer data; do not
          degrade service for other customers; respect the 90-day coordinated-disclosure norm unless
          we agree on a different timeline.
        </p>
      </Prose>
    </section>
  );
}
