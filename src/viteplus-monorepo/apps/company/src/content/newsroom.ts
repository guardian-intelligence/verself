// Newsroom items — content source for the /newsroom index, the
// /newsroom/$slug article route, and the homepage NewsroomStrip.
//
// Doctrine (per /design/newsroom): Flare on a homepage or cross-treatment
// surface always means "Guardian is speaking in the public register." If it
// is on the page, something newsworthy sits inside it. No decorative Flare.
// When there is no current item the strip and the index hero both render
// empty states.
//
// Shape: every entry is a full bulletin with a body. Add a new bulletin by
// prepending to ITEMS (the array is reverse-chronological by convention).

export type NewsroomCategory = "announcement" | "milestone" | "note";

export interface NewsroomAuthor {
  readonly name: string;
  readonly role: string;
  readonly avatar?: string;
}

export interface NewsroomItem {
  readonly slug: string;
  readonly kicker: string;
  readonly category: NewsroomCategory;
  readonly title: string;
  readonly deck: string;
  readonly date: string;
  readonly publishedAt: string;
  readonly author: NewsroomAuthor;
  readonly body: readonly string[];
  readonly ctaLabel?: string;
  readonly ctaHref?: string;
}

const ITEMS: readonly NewsroomItem[] = [
  {
    slug: "we-opened-the-newsroom",
    kicker: "Announcement",
    category: "announcement",
    title: "We opened the Newsroom.",
    deck: "Bulletins, milestones, and public notes from Guardian now have a home of their own, distinct from the long-form register of Letters.",
    date: "23 April 2026",
    publishedAt: "2026-04-23",
    author: { name: "Shovon Hasan", role: "Founder & CEO", avatar: "/people/shovon-hasan.jpg" },
    body: [
      "The house has three rooms. Workshop is where the work happens — Iron ground, Geist everywhere, Amber as the single accent. Letters is where we argue — Paper, Fraunces, one Bordeaux rule for the pull-quote. Today the middle room opens. Newsroom is where Guardian speaks in the public register: short, dated, on the record.",
      "We split it out because the registers were colliding. Bulletins about a milestone or a launch were showing up alongside essays about why the house exists, and the two read at different speeds. An announcement wants a headline and a timestamp. An essay wants a deck and a margin. A single surface cannot serve both without dulling one of them. So we gave the announcement its own room, on its own ground, with its own rhythm.",
      "The Newsroom ground is Argent. Flare appears in bounded bands — a hero card for the current bulletin, a subscribe strip at the foot of the page — and nowhere else. An acid-green page teaches the eye to stop seeing green; an acid-green band inside a white room teaches the eye to notice the band. Bordeaux and Amber never appear here. Fraunces stays because the Newsroom still carries the house voice.",
      "What belongs in the Newsroom: shipped features that change the public contract, milestones that customers and partners should hear about from us before they hear about it from anyone else, corrections and public notes, and the occasional announcement that is too short for a Letter and too material for a changelog entry. What does not belong: argument, commentary, or anything that asks the reader to sit down for five minutes. Those stay in Letters.",
      "The cadence is deliberate. Guardian speaks rarely. When the second bulletin files, it will land above this one and the archive grid will fill in under it. Until then, this is the whole room.",
    ],
    ctaLabel: "Read the bulletin",
  },
  {
    slug: "brand-system-shipped",
    kicker: "Milestone",
    category: "milestone",
    title: "Three rooms, one house.",
    deck: "Workshop, Newsroom, and Letters are now the three treatments that carry Guardian across every surface — and the design page walks the whole system.",
    date: "19 April 2026",
    publishedAt: "2026-04-19",
    author: { name: "Shovon Hasan", role: "Founder & CEO", avatar: "/people/shovon-hasan.jpg" },
    body: [
      "The brand model collapsed to three rooms. Each one paints its own ground, binds its own display font, and carries its own accent. A single data attribute on the nearest ancestor resolves every token downstream, which means the same page shell renders three different registers depending on where it sits in the site.",
      "Workshop is the everyday register — the product chrome, the marketing site, the console. Newsroom is the broadcast register — this room. Letters is the editorial register — where long-form lives. The rooms share a wordmark, a wings mark, and a single typographic idea; everything else is the treatment's choice.",
      "The /design page walks the whole system. Each room has a specimen card; step into the room and you see it inhabited.",
    ],
    ctaLabel: "See the rooms",
    ctaHref: "/design",
  },
  {
    slug: "letters-is-live",
    kicker: "Milestone",
    category: "milestone",
    title: "Letters is live.",
    deck: "The editorial register shipped with a seeded essay. Letters is the place where we explain the why.",
    date: "12 April 2026",
    publishedAt: "2026-04-12",
    author: { name: "Shovon Hasan", role: "Founder & CEO", avatar: "/people/shovon-hasan.jpg" },
    body: [
      "Letters ships on Paper. Ink type, Fraunces display, Bordeaux reserved for the single pull-quote rule — nothing else on the page is allowed that colour. The register is periodical: we publish when we have something to say, not on a calendar.",
      "The first letter is out today. The index lives at /letters and reads like a gazette.",
    ],
    ctaLabel: "Open Letters",
    ctaHref: "/letters",
  },
  {
    slug: "observability-in-public",
    kicker: "Note",
    category: "note",
    title: "Observability, in public.",
    deck: "Every route on this site emits a trace that lands in our own ClickHouse, on the same pipeline our customers use. We publish what we see there.",
    date: "5 April 2026",
    publishedAt: "2026-04-05",
    author: { name: "Shovon Hasan", role: "Founder & CEO", avatar: "/people/shovon-hasan.jpg" },
    body: [
      "The site runs on the same telemetry surface as the rest of the platform. Every route mount, every card click, every subscribe submit is a span. The spans land in ClickHouse. The same evidence we ask of our own services we ask of our own website.",
      "The point is not that we have telemetry. The point is that when we say a feature shipped and works, a queryable artifact says so.",
    ],
  },
];

// Reverse-chronological by convention. Returned read-only so call sites can't
// mutate the module-level array.
export function sortedNewsroomItems(): readonly NewsroomItem[] {
  return ITEMS;
}

export function currentNewsroomItem(): NewsroomItem | undefined {
  return ITEMS[0];
}

export function newsroomItemBySlug(slug: string): NewsroomItem | undefined {
  return ITEMS.find((item) => item.slug === slug);
}

// The default deep link for a bulletin is its article route. Callers can
// override via ctaHref for the rare bulletin that tees up a non-article
// destination (a design page, an external launch page, etc.).
export function newsroomCtaHref(item: NewsroomItem): string {
  return item.ctaHref ?? `/newsroom/${item.slug}`;
}

export function newsroomCtaLabel(item: NewsroomItem): string {
  return item.ctaLabel ?? "Read the bulletin";
}

export const NEWSROOM_META = {
  title: "Newsroom — Guardian",
  description: "Bulletins, milestones, and public notes from Guardian Intelligence.",
  siteURL: "https://guardianintelligence.org",
} as const;

export const CATEGORY_LABELS: Record<NewsroomCategory, string> = {
  announcement: "Announcements",
  milestone: "Milestones",
  note: "Notes",
};
