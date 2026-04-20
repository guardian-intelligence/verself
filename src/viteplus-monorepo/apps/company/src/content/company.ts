// About. A short page. Phase 4 ships a skeleton; future expansions add team,
// timeline, press coverage, and the investor-deck-equivalent detail set.

export const COMPANY_META = {
  title: "Company — Guardian Intelligence",
  description:
    "Guardian Intelligence is an American applied intelligence company, based in Seattle. We build the reference architecture for one-founder companies.",
} as const;

export const company = {
  kicker: "Seattle, Washington · Est. 2026",
  hero: "We build the reference architecture for one-founder companies.",
  paragraphs: [
    "Guardian Intelligence is an American applied intelligence firm. We build compute, integrations, and founder tooling — the systems every company needs before it can build what its founders actually started the company to build.",
    "We run ourselves on our own platform. Every service we ship, we use. Every billing flow, we receive an invoice against. Every identity edge, we hit. That is the only way to learn what a real customer experiences.",
    "We open-source per subdirectory. Each service in the repo has its own AGENTS.md, its own OpenAPI surface, its own migrations and docs. Forking a single service does not require forking the whole company.",
  ],
  values: [
    {
      name: "Measured",
      body: "The work is the argument. We ship, we document, we explain, and we leave the judgment to the reader.",
    },
    {
      name: "Grounded",
      body: "A specific noun or number in every paragraph. Software, like writing, is honest only when concrete.",
    },
    {
      name: "Unhurried",
      body: "White space is load-bearing. We would rather ship one right thing than three almost-right ones.",
    },
  ],
} as const;
