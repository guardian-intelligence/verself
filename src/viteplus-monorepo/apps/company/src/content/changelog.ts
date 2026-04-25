// Site changelog. Distinct from the product policy changelog (which lives
// at verself.sh/policy/changelog and is the legal-record of
// commitment changes) and from the product release notes (which live in
// each product's own surface). This is the company-site changelog: what
// landed on guardianintelligence.org and when.

export interface ChangelogEntry {
  readonly date: string;
  readonly title: string;
  readonly body: string;
}

export const CHANGELOG_META = {
  title: "Changelog — Guardian",
  description: "What shipped on guardianintelligence.org and when.",
} as const;

export const changelog: readonly ChangelogEntry[] = [
  {
    date: "2026-04-20",
    title: "Solutions replace Products. Trust and Legal move to Verself.",
    body: "The public IA collapses to a single Solution — Verself Platform — and the /products route is retired. Verself Platform is the bundle a customer buys; services, the web console, CLIs, and SDKs are its products and are described on Verself's own surfaces. The /trust and /legal routes are retired on guardianintelligence.org; terms, privacy, the SLA, subprocessors, data retention, and security disclosures live with Verself at verself.sh/policy where the data is actually processed. The marketing site keeps its company-level surfaces: Letters, Design, Press, Careers, Changelog, Contact.",
  },
  {
    date: "2026-04-19",
    title: "guardianintelligence.org moves to apps/company",
    body: "The Guardian company site gets its own TanStack Start app, separate from the Verself product surface. Verself docs and policy live at verself.sh, while the authenticated console lives at console.verself.sh.",
  },
];
