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
        <SummaryItem term="Isolation">Firecracker microVMs + ZFS dataset separation.</SummaryItem>
        <SummaryItem term="Identity">
          Zitadel; JWT-based; every service is its own security boundary.
        </SummaryItem>
        <SummaryItem term="Transport">TLS 1.3 everywhere.</SummaryItem>
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
          Zitadel is the sole identity provider. Every Go service validates JWTs against Zitadel's
          JWKS endpoint using cached keys and local crypto; identity (subject, organization, roles)
          is extracted from token claims and attached to the request context. No long-lived
          credentials live in services; short-lived JWTs replace them.
        </p>
        <p>
          Organization administrators can require MFA and passkeys through the identity console.
          Zitadel session configuration is not something customers edit indirectly via Forge Metal;
          they own their identity configuration end-to-end.
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
          Customer workloads run in Firecracker microVMs under jailer, with per-tenant ZFS datasets
          as durable storage. VM-to-VM isolation is a hypervisor-level boundary; host access is
          restricted to the privileged vm-orchestrator daemon. Product services do not have host
          access to <code>/dev/kvm</code>, <code>/dev/zvol</code>, jailer directories, or ZFS
          administrative commands; they interact with the substrate only via the orchestrator's gRPC
          API.
        </p>
        <p>
          Execution telemetry is collected via a 60-Hz vsock-based Zig guest agent; nothing on the
          guest side has host-network egress by default.
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
          All external and inter-service traffic uses TLS 1.3, terminated by Caddy with a Coraza WAF
          module; certificates are issued automatically via DNS-01 against Cloudflare. Secrets are
          SOPS-encrypted in the platform inventory and delivered to each service through systemd{" "}
          <code>LoadCredential=</code>, so application processes read them from the ephemeral{" "}
          <code>$CREDENTIALS_DIRECTORY</code> rather than from the filesystem.
        </p>
        <p>
          ZFS dataset-level encryption is supported for durable customer volumes; the specific
          encryption posture per volume is visible on the volume's billing record.
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
          The host uses nftables to deny by default and allow on a per-service basis; services
          listen only where their role declares they should. The single-node topology keeps
          inter-service traffic on loopback; the 3-node topology uses a Netbird overlay with
          analogous allowlists. Customer-facing ingress is TLS-terminated at Caddy before reaching
          Go services.
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
          Every service emits OpenTelemetry logs, traces, and metrics into ClickHouse via the
          otelcol-contrib collector. Ansible playbook runs emit spans that correlate to the same
          trace-id as the service calls they trigger, so post-hoc inspection of a deploy is
          straightforward.
        </p>
        <p>
          Detection thresholds and alert routes are configured in Grafana and checked through the{" "}
          <code>make observe</code> operator surface. Security-incident artifacts are retained
          separately from normal operational TTLs — see the{" "}
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
          Access to the host and to production data is restricted to a named set of operators on the
          account, who authenticate with hardware security keys. Administrative actions taken on the
          host are audit-logged and fed into the same OpenTelemetry pipeline as service traces.
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
          we agree a different timeline.
        </p>
      </Prose>
    </section>
  );
}
