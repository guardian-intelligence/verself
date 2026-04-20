// Trust. A thin public commitments surface for the company. The full legal
// tree (Terms, DPA, Acceptable Use, Privacy, Security, SLA, Subprocessors,
// Data Retention, Cookies, Policy Changelog) lives with Metal at
// platform.anveio.com/policy because that is where customer data actually
// flows. This page summarises the commitments and links to the canonical
// documents for customers who want more.

export const TRUST_META = {
  title: "Trust — Guardian Intelligence",
  description:
    "Guardian Intelligence's public commitments about data handling, security, availability, and vendor relationships.",
} as const;

export const trust = {
  kicker: "Our commitments to the people we serve.",
  hero: "We tell you what we do with your data, and then we do that.",
  intro:
    "Guardian Intelligence is a small company that handles real data for real customers. This page summarises what we commit to. The long form is kept alongside the product that actually processes the data, at platform.anveio.com/policy.",
  commitments: [
    {
      title: "We hold the minimum we need.",
      body: "Each service stores exactly what it needs to do its job. Billing stores billing. Identity stores identity. Mailbox stores mail. Services do not share databases.",
      canonical: {
        label: "Data retention",
        href: "https://platform.anveio.com/policy/data-retention",
      },
    },
    {
      title: "We encrypt at rest and in transit.",
      body: "Customer data is encrypted at rest on every service that stores it. External communication is TLS 1.2 or newer. Internal communication between services is either same-host loopback or encrypted.",
      canonical: {
        label: "Security overview",
        href: "https://platform.anveio.com/policy/security",
      },
    },
    {
      title: "We name every subprocessor.",
      body: "The third parties we engage are listed by name, role, and region. The list is maintained alongside the product.",
      canonical: {
        label: "Subprocessors",
        href: "https://platform.anveio.com/policy/subprocessors",
      },
    },
    {
      title: "We report outages before we are asked.",
      body: "Material incidents are posted to the changelog with a description, a timeline, and a remediation plan. We do not quietly recover.",
      canonical: {
        label: "Service level agreement",
        href: "https://platform.anveio.com/policy/sla",
      },
    },
    {
      title: "We answer every security report.",
      body: "Security contact is listed below. Our /.well-known/security.txt describes the disclosure path. Every report gets a human response.",
      canonical: {
        label: "Security contact",
        href: "mailto:security@anveio.com",
      },
    },
  ],
  footerNote:
    "The canonical legal tree — Terms of Service, Privacy Policy, Data Processing Addendum, Acceptable Use Policy, Cookie Policy, and the full Policy Changelog — lives with Metal at platform.anveio.com/policy. That is also where customer data is actually processed.",
} as const;
