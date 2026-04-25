// Letters — Guardian's long-form. One seeded letter ships alongside the
// scaffold so the index, the letter route, and the RSS feed all render real
// content on the first deploy instead of empty states.
//
// Adding a letter: append an entry to LETTERS with a unique slug and a body as
// an array of paragraphs. The voice lint scans every paragraph on build.

export interface Letter {
  readonly slug: string;
  readonly title: string;
  readonly kicker: string;
  readonly publishedAt: string;
  readonly author: string;
  readonly summary: string;
  readonly body: readonly string[];
}

export const LETTERS_META = {
  title: "Letters — Guardian",
  description:
    "Long-form from Guardian. Published when we have something to say, not on a calendar.",
  editor: "Guardian",
  siteURL: "https://guardianintelligence.org",
} as const;

export const LETTERS: readonly Letter[] = [
  {
    slug: "ship-the-reference-architecture",
    kicker: "A note from the founders",
    title: "Ship the reference architecture.",
    publishedAt: "2026-04-19",
    author: "Guardian",
    summary:
      "Every founder spends the first year on the same dozen systems. We ship them, open-source, per subdirectory, so the second founder never has to.",
    body: [
      "We started Guardian to do two things: run our own company with as few people as possible, and open-source the formula for everyone else.",
      "The first year of any company is spent on the same dozen systems. Identity. Billing. Analytics. Email. Infrastructure. Security. The thousand edges where a real company touches the real world. None of it is what a founder started the company to build. All of it has to be right.",
      "The open-source world is rich in primitives and thin in assemblies. There are a hundred identity providers, a hundred billing systems, a hundred metrics pipelines. There is no single codebase that takes all of them, wires them together the way a real company would, and then operates itself on that codebase.",
      "We build that codebase. The repo is one per subdirectory — platform, mailbox-service, billing-service, identity-service, sandbox-rental-service, vm-orchestrator, and the pieces that hold them together. We dogfood every service on the same substrate our customers use. Letters is the place where we talk about why and how.",
      "The first customer of Guardian is Guardian.",
    ],
  },
];

export function letterBySlug(slug: string): Letter | undefined {
  return LETTERS.find((letter) => letter.slug === slug);
}

export function sortedLetters(): readonly Letter[] {
  return [...LETTERS].sort((a, b) => (a.publishedAt < b.publishedAt ? 1 : -1));
}
