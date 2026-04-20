// Legal. This is the company-site mirror — a short explanation of the split
// and a link out to the canonical legal tree at platform.anveio.com/policy.
// Metal, not the marketing site, is the data processor; the canonical tree
// lives with the processor.

export const LEGAL_META = {
  title: "Legal — Guardian Intelligence",
  description:
    "The Guardian Intelligence legal stack. The full tree lives with Metal at platform.anveio.com/policy.",
} as const;

export const legal = {
  kicker: "The short version.",
  hero: "Terms, privacy, and policy live with Metal.",
  intro:
    "Guardian Intelligence runs the marketing site at anveio.com and the product (Metal) at platform.anveio.com. The legal tree — the Terms of Service you sign, the Privacy Policy that binds us, the Data Processing Addendum your counsel will read — lives with Metal because Metal is what actually processes customer data. This page points to every document there.",
  mirrors: [
    {
      title: "Terms of Service",
      body: "The master agreement under which customer organisations use the platform.",
      href: "https://platform.anveio.com/policy/terms",
    },
    {
      title: "Privacy Policy",
      body: "How we handle personal data, the controller/processor split, and data subject rights.",
      href: "https://platform.anveio.com/policy/privacy",
    },
    {
      title: "Data Processing Addendum",
      body: "Processor obligations under GDPR Article 28 and the incorporated Standard Contractual Clauses.",
      href: "https://platform.anveio.com/policy/dpa",
    },
    {
      title: "Acceptable Use Policy",
      body: "What is and is not allowed on the substrate.",
      href: "https://platform.anveio.com/policy/acceptable-use",
    },
    {
      title: "Cookie Policy",
      body: "Cookies set on customer-facing web surfaces and why each is strictly necessary.",
      href: "https://platform.anveio.com/policy/cookies",
    },
    {
      title: "Subprocessors",
      body: "The third parties we engage, what data they process, and where.",
      href: "https://platform.anveio.com/policy/subprocessors",
    },
    {
      title: "Security",
      body: "Technical and organisational measures for securing customer data and workloads.",
      href: "https://platform.anveio.com/policy/security",
    },
    {
      title: "Data Retention",
      body: "What we keep, how long, and how it is exported or deleted.",
      href: "https://platform.anveio.com/policy/data-retention",
    },
    {
      title: "SLA",
      body: "Availability commitments for the current topology and the roadmap.",
      href: "https://platform.anveio.com/policy/sla",
    },
    {
      title: "Policy Changelog",
      body: "Every material change to the documents above, with the diff that took it live.",
      href: "https://platform.anveio.com/policy/changelog",
    },
  ],
} as const;
