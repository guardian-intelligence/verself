// Newsroom items — content source for NewsroomStrip.
//
// The rule, per /design/newsroom: Flare on a homepage or cross-treatment
// surface always means "Guardian is speaking in the public register." If
// it is on the page, something newsworthy sits inside it. No decorative
// Flare. When there is no current item, the strip renders nothing.
//
// When a new milestone, release, or public note lands, prepend it here;
// the homepage picks up the latest `current` automatically. Older items
// can migrate to a future /newsroom index without changing this module's
// shape.

export interface NewsroomItem {
  readonly slug: string;
  readonly kicker: string;
  readonly title: string;
  readonly date: string;
  readonly ctaLabel: string;
  readonly ctaHref: string;
}

const ITEMS: readonly NewsroomItem[] = [
  {
    slug: "brand-system-2026-04",
    kicker: "Brand system",
    title: "Four treatments, one house.",
    date: "19 April 2026",
    ctaLabel: "See the rooms",
    ctaHref: "/design",
  },
];

export function currentNewsroomItem(): NewsroomItem | undefined {
  return ITEMS[0];
}
