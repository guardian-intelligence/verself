// Contact. No form. Email addresses that go to real humans.

export const CONTACT_META = {
  title: "Contact — Guardian",
  description:
    "Email addresses for sales, press, security, and careers. No form. We answer every note.",
} as const;

export const contact = {
  kicker: "We answer every note.",
  hero: "Contact Guardian.",
  intro:
    "There is no form. Every address below goes to a person. We try to answer within one working day.",
  channels: [
    {
      name: "General",
      email: "hello@guardianintelligence.org",
      note: "Everything that doesn't fit the other buckets.",
    },
    {
      name: "Sales",
      email: "sales@guardianintelligence.org",
      note: "You want to run real work on Metal Platform.",
    },
    {
      name: "Press",
      email: "press@guardianintelligence.org",
      note: "Journalists and editors writing about Guardian.",
    },
    {
      name: "Security",
      email: "security@guardianintelligence.org",
      note: "Anything that concerns customer data or platform safety is handled with Metal; this address is the marketing-site backstop and we route from here.",
    },
    {
      name: "Careers",
      email: "careers@guardianintelligence.org",
      note: "You would like to work with us.",
    },
  ],
  mailingAddress: "Guardian Intelligence · Seattle, Washington, USA",
} as const;
