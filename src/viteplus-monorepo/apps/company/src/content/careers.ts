// Careers. Guardian is small. The page is a real page, not a "we are hiring"
// modal — it explains who we hire, when, and what a week looks like.

export const CAREERS_META = {
  title: "Careers — Guardian Intelligence",
  description:
    "Guardian Intelligence is small on purpose. We hire slowly, in person, and for work that compounds.",
} as const;

export const careers = {
  kicker: "We hire slowly, in person, and for work that compounds.",
  hero: "We hire slowly.",
  paragraphs: [
    "Guardian Intelligence is small on purpose. The thesis of the company is that a small number of people, paired with modern tooling, can ship what used to require a floor of engineers. We staff to that thesis.",
    "When we hire, we hire for work that compounds. Authors of services, not authors of tickets. Writers of the Dispatch, not attendees of standups. Operators who would rather run a thing well than manage a team running a thing.",
    "We post openings as they open. There is no talent pool, no pipeline, no recruiter cadence. If the page below is empty, we are not hiring right now.",
  ],
  openings: [] as readonly { title: string; description: string }[],
  emptyState:
    "No open roles at the moment. Write to us anyway if you would enjoy the work described above — we answer every note.",
  contactEmail: "careers@guardianintelligence.org",
} as const;
