// Letters — Guardian's long-form. The seeded set ships alongside the scaffold
// so the gazette renders with real density on the first deploy instead of an
// empty state.
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
    title: "Ship the Reference Architecture",
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
  {
    slug: "the-first-customer-is-us",
    kicker: "Operations",
    title: "The First Customer Is Us",
    publishedAt: "2026-04-15",
    author: "Guardian",
    summary:
      "Every service we sell, we run a real bill against. The platform org carries a 100% discount and a real invoice. The math is what teaches us what is broken.",
    body: [
      "Guardian models itself as a tenant on its own platform. Every API our customers call, we call the same way, through the same gateway, with the same rate limits. The platform org receives a showback invoice each month with a 100% discount applied. The line items are real.",
      "The invoice is the point. A discounted bill is still an audit. When the metering pipeline drops a row, our showback shows the gap before a customer ever notices. When the rate limiter mis-reads a tenant header, our own dashboards page first. The economics of the platform become a debugging surface.",
      "The architecture pressure is the same. We have to talk to ourselves over the wire because we have to talk to ourselves the way a stranger would. There is no private back door from the company site to the platform, no shortcut from the marketing copy to the billing ledger. The wires are the contract.",
      "It is slower in the short run. It is the only way the long run works.",
    ],
  },
  {
    slug: "errors-as-data",
    kicker: "Engineering",
    title: "Errors as Data",
    publishedAt: "2026-04-08",
    author: "Guardian",
    summary:
      "A failure is a row, not an apology. We tag every error with structure so the system can answer questions without a human in the loop.",
    body: [
      "We treat errors as data. Every failure carries a tag, a code, and enough structure to be queried. The error is not a string in a log; it is a row with columns we can group and count.",
      "The shape lets the platform answer questions without a person in the room. Which tenants saw a 429 in the last hour. Which routes failed because a dependency was cold. Which background job failed and how many retries it took to clear. The questions are routine. The answers are SQL.",
      "The discipline is upstream of the dashboard. A function that returns a tagged error is a function whose callers can branch on the tag. A function that returns a sentence is a function whose callers can only log it. The first scales; the second accumulates.",
      "We write the tags before we write the dashboards. The dashboards are what falls out.",
    ],
  },
  {
    slug: "white-space-is-load-bearing",
    kicker: "Brand",
    title: "White Space Is Load-Bearing",
    publishedAt: "2026-04-01",
    author: "Guardian",
    summary:
      "A page with room around the words trusts the reader. A page without it does not. We design for the second reading, not the first scroll.",
    body: [
      "A page with room around the words trusts the reader. A page without it does not. The most expensive thing on a page is the next sentence after the one the reader almost stopped at, and the cheapest way to keep that sentence is to leave the room around it alone.",
      "We design for the second reading. The first reading is a scroll; the second is a pause. The first rewards motion and headlines; the second rewards measure and air. A surface that survives the second reading earns the third.",
      "The rule extends to chrome. A border is punctuation, not architecture. A divider that does not earn its line should not draw one. The eye reads structure from rhythm before it reads it from rules; rhythm is cheaper, quieter, and harder to get wrong.",
      "We are not building a magazine. We are building a company. The page is the surface where the company introduces itself to a stranger. The stranger gets one read. The page should be unhurried.",
    ],
  },
];

export function letterBySlug(slug: string): Letter | undefined {
  return LETTERS.find((letter) => letter.slug === slug);
}

export function sortedLetters(): readonly Letter[] {
  return [...LETTERS].sort((a, b) => (a.publishedAt < b.publishedAt ? 1 : -1));
}
