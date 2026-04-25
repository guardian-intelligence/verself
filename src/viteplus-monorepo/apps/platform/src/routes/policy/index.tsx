import { createFileRoute, Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";

import { effectiveDateOf, formatPrettyDate, type PolicyId } from "~/lib/policy-catalog";

export const Route = createFileRoute("/policy/")({
  component: PolicyOverview,
  head: () => ({
    meta: [
      { title: "Policy — Verself Platform" },
      {
        name: "description",
        content: "Customer-facing policy documents for the Verself platform.",
      },
    ],
  }),
});

type PolicyCardSpec = {
  readonly to: string;
  readonly policyId: PolicyId;
  readonly title: string;
  readonly description: string;
};

const POLICIES: readonly PolicyCardSpec[] = [
  {
    to: "/policy/terms",
    policyId: "terms",
    title: "Terms of Service",
    description: "The master agreement under which customer organizations use the platform.",
  },
  {
    to: "/policy/privacy",
    policyId: "privacy",
    title: "Privacy Policy",
    description:
      "How personal data is handled, the controller/processor split, and the rights of data subjects.",
  },
  {
    to: "/policy/acceptable-use",
    policyId: "acceptable-use",
    title: "Acceptable Use",
    description: "Workloads, traffic patterns, and content prohibited on the substrate.",
  },
  {
    to: "/policy/dpa",
    policyId: "dpa",
    title: "Data Processing Addendum",
    description:
      "Processor obligations under GDPR Art. 28 and the incorporated Standard Contractual Clauses.",
  },
  {
    to: "/policy/subprocessors",
    policyId: "subprocessors",
    title: "Subprocessors",
    description:
      "The third parties we engage to operate the service, the data they process, and where.",
  },
  {
    to: "/policy/security",
    policyId: "security",
    title: "Security Overview",
    description: "Technical and organizational measures for securing customer data and workloads.",
  },
  {
    to: "/policy/sla",
    policyId: "sla",
    title: "Service Level Agreement",
    description: "Availability commitments for the current topology and the roadmap.",
  },
  {
    to: "/policy/cookies",
    policyId: "cookies",
    title: "Cookie Policy",
    description: "Cookies set on customer-facing web surfaces and why each is strictly necessary.",
  },
  {
    to: "/policy/data-retention",
    policyId: "data-retention",
    title: "Data Retention",
    description:
      "What we keep, how long we keep it, and how it is exported or deleted across the account lifecycle.",
  },
];

function PolicyOverview() {
  return (
    <div className="flex flex-col gap-10">
      <header className="flex flex-col gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Platform Policy
        </p>
        <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">Policy</h1>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base md:leading-7">
          Verself's commitments to customers about how we handle your data, your account, and the
          services we run on your behalf. Every document below applies to every organization on this
          deployment, including our own. Prior versions and the diff that took them live are at{" "}
          <Link to="/policy/changelog">/policy/changelog</Link>.
        </p>
      </header>

      <ul className="flex flex-col gap-2">
        {POLICIES.map((policy) => (
          <PolicyCard key={policy.policyId} {...policy} />
        ))}
      </ul>
    </div>
  );
}

function PolicyCard({ to, title, description, policyId }: PolicyCardSpec) {
  const effective = `Effective ${formatPrettyDate(effectiveDateOf(policyId))}`;
  return (
    <li>
      <Link
        to={to}
        className="group flex items-start gap-4 rounded-lg border border-border bg-card p-5 transition-colors hover:border-foreground/30"
      >
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="font-medium">{title}</span>
            <span className="text-xs text-muted-foreground">{effective}</span>
          </div>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-colors group-hover:text-foreground" />
      </Link>
    </li>
  );
}
