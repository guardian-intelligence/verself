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

export const Route = createFileRoute("/policy/acceptable-use")({
  component: AUP,
  loader: () => getOperatorDomain(),
  head: () => ({
    meta: [
      { title: "Acceptable Use Policy — Forge Metal Platform" },
      {
        name: "description",
        content:
          "Workloads, traffic patterns, and content prohibited on the Forge Metal substrate, and how violations are handled.",
      },
    ],
  }),
});

function AUP() {
  const operatorDomain = Route.useLoaderData();
  return (
    <PolicyArticle>
      <PolicyHeader title="Acceptable Use Policy" policyId="acceptable-use" />
      <Summary />
      <Scope />
      <Prohibited />
      <Network />
      <Mail />
      <Sandbox />
      <Reporting />
      <Enforcement />
      <ChangesSection policyId="acceptable-use" />
      <ContactSection operatorDomain={operatorDomain} primary="abuse" />
    </PolicyArticle>
  );
}

function Summary() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="summary">Summary</SectionHeading>
      <SummaryPanel>
        <SummaryItem term="Customer responsibility">
          You are responsible for everything your organization runs on Forge Metal, including
          workloads started by users, automation, and integrations.
        </SummaryItem>
        <SummaryItem term="No illegal content">
          — including CSAM, content that violates export control, and content that facilitates
          violence or stalking.
        </SummaryItem>
        <SummaryItem term="No platform abuse">
          — credential stuffing, spam, DDoS launch-points, unauthorized scraping, crypto mining,
          jailbreak-farm operations.
        </SummaryItem>
        <SummaryItem term="Enforcement">
          ranges from warning to account suspension and, for serious violations, termination. Urgent
          risks are mitigated immediately.
        </SummaryItem>
      </SummaryPanel>
    </section>
  );
}

function Scope() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="scope">Scope</SectionHeading>
      <Prose>
        <p>
          This policy applies to every workload, binary, network request, piece of content, and
          communication originated by or through the customer's organization on the Forge Metal
          substrate. "Customer" here means the organization identified on the account, including its
          users, automation, and third parties it authorizes.
        </p>
      </Prose>
    </section>
  );
}

function Prohibited() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="prohibited">Prohibited content and use</SectionHeading>
      <Prose>
        <p>The following are prohibited without exception:</p>
        <ul>
          <li>
            Child sexual abuse material (CSAM), or any content depicting or soliciting the sexual
            exploitation of minors. Reports are forwarded to NCMEC and law enforcement immediately.
          </li>
          <li>
            Content or operations that violate applicable export-control law (U.S. EAR, ITAR,
            OFAC-sanctioned destinations, analogous regimes in other jurisdictions).
          </li>
          <li>
            Content that promotes or facilitates imminent violence, terrorism, or targeted
            harassment or stalking of a named person.
          </li>
          <li>
            Malware, ransomware, command-and-control infrastructure, or the live hosting of
            credential-phishing sites.
          </li>
          <li>
            Unauthorized access operations — including port-scanning, credential stuffing,
            brute-force login, and exploit development or detonation against systems you do not own
            or have written permission to test.
          </li>
          <li>
            Spam, unsolicited bulk email, SMS blast campaigns, or abuse of transactional email
            delivery.
          </li>
          <li>
            Cryptocurrency mining and high-intensity proof-of-work operations unrelated to a
            workload the customer has declared to us and that is separately priced.
          </li>
          <li>
            Operations designed to evade another service's rate limits or Terms of Service,
            including but not limited to "jailbreak-farm" style LLM API abuse, scraping that
            circumvents technical access controls, or the distribution of ban-evasion services.
          </li>
          <li>
            Content that infringes the intellectual property rights of others without license, or
            that misuses trademarks in a way likely to cause consumer confusion.
          </li>
        </ul>
      </Prose>
    </section>
  );
}

function Network() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="network">Network and resource abuse</SectionHeading>
      <Prose>
        <p>
          Workloads must not be used to originate denial-of-service attacks, reflection
          amplification, or sustained traffic patterns designed to degrade third-party
          infrastructure. Sustained resource usage materially in excess of an account's declared
          workload profile may be rate-limited while we discuss the workload with you.
        </p>
      </Prose>
    </section>
  );
}

function Mail() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="mail">Email and messaging</SectionHeading>
      <Prose>
        <p>
          Outbound email sent through Forge Metal must comply with the CAN-SPAM Act, GDPR lawful
          marketing bases, and the policies of our upstream email-delivery subprocessor. Inbound
          mail delivered to customer mailboxes is the customer's to handle; automated campaigns,
          subscribe-on-behalf-of-third-parties, and credential-harvesting via email are prohibited.
        </p>
      </Prose>
    </section>
  );
}

function Sandbox() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="sandbox">Sandbox and VM workloads</SectionHeading>
      <Prose>
        <p>
          Our sandbox and long-running VM products are isolation-hardened via hardware
          virtualization and per-tenant durable storage, but that isolation is not a license for
          hostile workloads. You may not attempt to break out of your VM boundary, probe other
          tenants, or exploit known hypervisor or host-kernel vulnerabilities. Bona-fide security
          research against your own tenant is encouraged under{" "}
          <a href="/policy/security">our Security Overview's disclosure terms</a>.
        </p>
      </Prose>
    </section>
  );
}

function Reporting() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="reporting">Reporting abuse</SectionHeading>
      <Prose>
        <p>
          If you believe a workload or account on Forge Metal is violating this policy — yours or
          someone else's — contact the abuse mailbox below. Reports are triaged within one business
          day; active abuse is mitigated sooner.
        </p>
      </Prose>
    </section>
  );
}

function Enforcement() {
  return (
    <section className="flex flex-col gap-4">
      <SectionHeading id="enforcement">Enforcement</SectionHeading>
      <Prose>
        <p>
          Responses to a violation range from warning, to targeted rate-limit, to account
          suspension, to termination, chosen proportionally to the severity of the violation and the
          risk to third parties. For imminent-risk situations (active DDoS origination, live CSAM,
          active credential-phishing), we take immediate mitigation action without prior notice and
          notify the administrators after the fact.
        </p>
        <p>
          A suspension triggered by an AUP violation follows the{" "}
          <a href="/policy/data-retention#lifecycle">account lifecycle</a>; export during the
          suspended state remains available unless the violation requires preserving evidence.
        </p>
      </Prose>
    </section>
  );
}
