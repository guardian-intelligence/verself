// Contact. No form. Email addresses that go to real humans.

export const CONTACT_META = {
  title: "Contact — Guardian Intelligence",
  description:
    "Email addresses for sales, press, security, and careers. No form. We answer every note.",
} as const;

export const contact = {
  kicker: "We answer every note.",
  hero: "Contact Guardian Intelligence.",
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
      note: "You want to use Metal, Console, or Letters for real work.",
    },
    {
      name: "Press",
      email: "press@guardianintelligence.org",
      note: "Journalists and editors writing about Guardian.",
    },
    {
      name: "Security",
      email: "security@anveio.com",
      note: "Disclosure path described in /.well-known/security.txt.",
    },
    {
      name: "Careers",
      email: "careers@guardianintelligence.org",
      note: "You would like to work with us.",
    },
  ],
  mailingAddress: "Guardian Intelligence · Seattle, Washington, USA",
} as const;
