// Site changelog. Distinct from the product policy changelog (which lives
// at platform.anveio.com/policy/changelog and is the legal-record of
// commitment changes) and from the product release notes (which live in
// each product's own surface). This is the company-site changelog: what
// landed on anveio.com and when.

export interface ChangelogEntry {
  readonly date: string;
  readonly title: string;
  readonly body: string;
}

export const CHANGELOG_META = {
  title: "Changelog — Guardian Intelligence",
  description: "What shipped on anveio.com and when.",
} as const;

export const changelog: readonly ChangelogEntry[] = [
  {
    date: "2026-04-19",
    title: "anveio.com moves to apps/company",
    body: "The Guardian Intelligence company site gets its own TanStack Start app, separate from the Metal platform console. Landing, /design, /dispatch, /products, /company, /careers, /press, /trust, /changelog, /contact, and /legal all live at anveio.com. The Metal console moves to platform.anveio.com and will eventually resolve at console.<domain>.",
  },
];
