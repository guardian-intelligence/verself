// Single source of truth for the /policy left rail. Same shape as
// DOCS_NAV so the rail component can stay symmetrical.
export type PolicyNavChild = {
  readonly id: string;
  readonly label: string;
};

export type PolicyNavEntry = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly matchPrefix: string;
  readonly exactMatch?: boolean;
  readonly children?: readonly PolicyNavChild[];
};

const TERMS_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "agreement", label: "Agreement" },
  { id: "accounts", label: "Accounts" },
  { id: "use", label: "Use" },
  { id: "fees", label: "Fees and invoicing" },
  { id: "content", label: "Your content" },
  { id: "services", label: "Service changes" },
  { id: "warranty", label: "Warranty" },
  { id: "liability", label: "Liability" },
  { id: "termination", label: "Termination" },
  { id: "law", label: "Governing law" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const PRIVACY_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "roles", label: "Roles" },
  { id: "collection", label: "What we collect" },
  { id: "ropa", label: "Processing activities" },
  { id: "sharing", label: "Who we share with" },
  { id: "dsr", label: "Data-subject requests" },
  { id: "transfers", label: "International transfers" },
  { id: "regional", label: "Regional supplements" },
  { id: "retention", label: "Retention" },
  { id: "security", label: "Security of processing" },
  { id: "children", label: "Children" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const AUP_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "scope", label: "Scope" },
  { id: "prohibited", label: "Prohibited use" },
  { id: "network", label: "Network abuse" },
  { id: "mail", label: "Email" },
  { id: "sandbox", label: "Sandbox and VMs" },
  { id: "reporting", label: "Reporting abuse" },
  { id: "enforcement", label: "Enforcement" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const DPA_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "scope", label: "Scope" },
  { id: "instructions", label: "Instructions" },
  { id: "confidentiality", label: "Confidentiality" },
  { id: "security", label: "Security of processing" },
  { id: "subprocessing", label: "Subprocessors" },
  { id: "transfers", label: "International transfers" },
  { id: "dsr-assistance", label: "DSR assistance" },
  { id: "incident", label: "Breach notification" },
  { id: "return-deletion", label: "Return & deletion" },
  { id: "audit", label: "Audit" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const SUBPROCESSORS_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "catalog", label: "Active subprocessors" },
  { id: "change-notification", label: "Change notification" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const SECURITY_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "identity", label: "Identity" },
  { id: "isolation", label: "Workload isolation" },
  { id: "encryption", label: "Encryption" },
  { id: "network", label: "Network posture" },
  { id: "logging", label: "Logging" },
  { id: "personnel", label: "Personnel" },
  { id: "disclosure", label: "Disclosure" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const SLA_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "current-tier", label: "Current tier" },
  { id: "maintenance", label: "Maintenance" },
  { id: "support", label: "Support" },
  { id: "future-tier", label: "Three-node tier" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const COOKIES_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "inventory", label: "Cookie inventory" },
  { id: "analytics", label: "Analytics" },
  { id: "controls", label: "Controls" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

export const DATA_RETENTION_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "scope", label: "Scope & definitions" },
  { id: "roles", label: "Roles" },
  { id: "lifecycle", label: "Account lifecycle" },
  { id: "retention", label: "Retention windows" },
  { id: "export", label: "Data export" },
  { id: "dsr", label: "Data-subject requests" },
  { id: "extensions", label: "Extensions" },
  { id: "legal-hold", label: "Legal hold" },
  { id: "deletion", label: "Final deletion" },
  { id: "anonymized", label: "Anonymized data" },
  { id: "incident", label: "Incident data" },
  { id: "operator", label: "Operator handling" },
  { id: "changes", label: "Changes" },
  { id: "contact", label: "Contact" },
];

const CHANGELOG_SECTIONS: readonly PolicyNavChild[] = [
  { id: "summary", label: "Summary" },
  { id: "entries", label: "Entries" },
  { id: "contact", label: "Contact" },
];

export const POLICY_NAV: readonly PolicyNavEntry[] = [
  {
    id: "overview",
    label: "Overview",
    to: "/policy",
    matchPrefix: "/policy",
    exactMatch: true,
  },
  {
    id: "terms",
    label: "Terms",
    to: "/policy/terms",
    matchPrefix: "/policy/terms",
    children: TERMS_SECTIONS,
  },
  {
    id: "privacy",
    label: "Privacy",
    to: "/policy/privacy",
    matchPrefix: "/policy/privacy",
    children: PRIVACY_SECTIONS,
  },
  {
    id: "acceptable-use",
    label: "Acceptable Use",
    to: "/policy/acceptable-use",
    matchPrefix: "/policy/acceptable-use",
    children: AUP_SECTIONS,
  },
  {
    id: "dpa",
    label: "DPA",
    to: "/policy/dpa",
    matchPrefix: "/policy/dpa",
    children: DPA_SECTIONS,
  },
  {
    id: "subprocessors",
    label: "Subprocessors",
    to: "/policy/subprocessors",
    matchPrefix: "/policy/subprocessors",
    children: SUBPROCESSORS_SECTIONS,
  },
  {
    id: "security",
    label: "Security",
    to: "/policy/security",
    matchPrefix: "/policy/security",
    children: SECURITY_SECTIONS,
  },
  {
    id: "sla",
    label: "SLA",
    to: "/policy/sla",
    matchPrefix: "/policy/sla",
    children: SLA_SECTIONS,
  },
  {
    id: "cookies",
    label: "Cookies",
    to: "/policy/cookies",
    matchPrefix: "/policy/cookies",
    children: COOKIES_SECTIONS,
  },
  {
    id: "data-retention",
    label: "Data retention",
    to: "/policy/data-retention",
    matchPrefix: "/policy/data-retention",
    children: DATA_RETENTION_SECTIONS,
  },
  {
    id: "changelog",
    label: "Changelog",
    to: "/policy/changelog",
    matchPrefix: "/policy/changelog",
    children: CHANGELOG_SECTIONS,
  },
];

export function isPathActive(
  currentPath: string,
  entry: { matchPrefix: string; exactMatch?: boolean },
): boolean {
  if (entry.exactMatch) {
    return currentPath === entry.matchPrefix;
  }
  return currentPath === entry.matchPrefix || currentPath.startsWith(`${entry.matchPrefix}/`);
}
